#!/usr/bin/env python3
"""
NiceHash API poller for Telegraf exec input.

Fetches mining payouts, rig status, and account balance from the NiceHash API v2,
then outputs InfluxDB line protocol for Telegraf to forward to QuestDB.

Usage:
    python3 nicehash_telegraf.py --config /path/to/nicehash_config.json

Config file (JSON):
    {
        "api_key": "your-api-key",
        "api_secret": "your-api-secret",
        "org_id": "your-organization-id"
    }

Generate API keys at: https://www.nicehash.com/my/settings/keys
Required permissions: Mining (view), Wallet (view)

Output measurements (InfluxDB line protocol):
    nicehash_payouts   - individual payout records
    nicehash_rigs      - per-rig status, unpaid balance, profitability
    nicehash_balance   - total account balance
"""

import argparse
import hmac
import json
import os
import sys
import time
import uuid
from datetime import datetime, timezone
from hashlib import sha256

try:
    import requests
except ImportError:
    print("ERROR: 'requests' package required. Install with: pip3 install requests", file=sys.stderr)
    sys.exit(1)

BASE_URL = "https://api2.nicehash.com"


def load_config(path):
    with open(path) as f:
        cfg = json.load(f)
    for key in ("api_key", "api_secret", "org_id"):
        if key not in cfg or not cfg[key]:
            print(f"ERROR: '{key}' missing in config file", file=sys.stderr)
            sys.exit(1)
    return cfg


def nicehash_request(cfg, method, path, query=""):
    """Make an authenticated NiceHash API v2 request."""
    xtime = str(int(time.time() * 1000))
    xnonce = str(uuid.uuid4())

    # Build HMAC input: key \0 time \0 nonce \0 \0 org_id \0 \0 method \0 path \0 query
    message = bytearray(cfg["api_key"], "utf-8")
    message += b"\x00" + bytearray(xtime, "utf-8")
    message += b"\x00" + bytearray(xnonce, "utf-8")
    message += b"\x00\x00" + bytearray(cfg["org_id"], "utf-8")
    message += b"\x00\x00" + bytearray(method, "utf-8")
    message += b"\x00" + bytearray(path, "utf-8")
    message += b"\x00" + bytearray(query, "utf-8")

    digest = hmac.new(bytearray(cfg["api_secret"], "utf-8"), message, sha256).hexdigest()

    headers = {
        "X-Time": xtime,
        "X-Nonce": xnonce,
        "X-Auth": cfg["api_key"] + ":" + digest,
        "X-Organization-Id": cfg["org_id"],
        "X-Request-Id": str(uuid.uuid4()),
    }

    url = BASE_URL + path
    if query:
        url += "?" + query

    resp = requests.get(url, headers=headers, timeout=15)
    resp.raise_for_status()
    return resp.json()


def escape_tag(value):
    """Escape special characters in InfluxDB tag values."""
    return str(value).replace(" ", "\\ ").replace(",", "\\,").replace("=", "\\=")


def escape_field_str(value):
    """Escape a string field value for InfluxDB line protocol."""
    return '"' + str(value).replace('"', '\\"') + '"'


def fetch_payouts(cfg):
    """Fetch recent payouts and output as InfluxDB line protocol."""
    lines = []
    try:
        data = nicehash_request(cfg, "GET", "/main/api/v2/mining/rigs/payouts", "size=10&page=0")
        payouts = data.get("list", [])
        for p in payouts:
            payout_id = p.get("id", "")
            amount = float(p.get("amount", 0))
            fee = float(p.get("feeAmount", 0))
            currency = p.get("currency", {})
            currency_name = currency.get("enumName", "BTC") if isinstance(currency, dict) else str(currency)
            # NiceHash timestamps are in milliseconds
            ts_ms = int(p.get("created", 0))
            if ts_ms == 0:
                continue
            ts_ns = ts_ms * 1_000_000  # convert ms to ns

            tags = f"currency={escape_tag(currency_name)}"
            fields = f"amount={amount},fee={fee},payout_id={escape_field_str(payout_id)}"
            lines.append(f"nicehash_payouts,{tags} {fields} {ts_ns}")
    except Exception as e:
        print(f"ERROR fetching payouts: {e}", file=sys.stderr)

    return lines


def fetch_rigs(cfg):
    """Fetch rig status and output as InfluxDB line protocol."""
    lines = []
    try:
        data = nicehash_request(cfg, "GET", "/main/api/v2/mining/rigs2", "size=50&page=0")
        now_ns = int(time.time() * 1e9)

        # Total unpaid amount for the whole account
        total_unpaid = float(data.get("unpaidAmount", 0))
        total_profitability = float(data.get("totalProfitability", 0))
        next_payout_ts = data.get("nextPayoutTimestamp")
        next_payout_str = ""
        if next_payout_ts:
            next_payout_str = str(next_payout_ts)

        fields = f"unpaid_total={total_unpaid},profitability_total={total_profitability}"
        if next_payout_str:
            fields += f",next_payout_ts={escape_field_str(next_payout_str)}"
        lines.append(f"nicehash_account {fields} {now_ns}")

        # Per-rig data
        rigs = data.get("miningRigs", [])
        for rig in rigs:
            rig_id = rig.get("rigId", "unknown")
            rig_name = rig.get("name", rig_id)
            status = rig.get("minerStatus", "UNKNOWN")
            unpaid = float(rig.get("unpaidAmount", 0))
            profitability = float(rig.get("profitability", 0))

            # Extract speed from stats if available
            speed_accepted = 0.0
            speed_rejected = 0.0
            stats = rig.get("stats", [])
            if stats:
                for stat in stats:
                    speed_accepted += float(stat.get("speedAccepted", 0))
                    speed_rejected += float(stat.get("speedRejected", 0))

            tags = f"rig_name={escape_tag(rig_name)},rig_id={escape_tag(rig_id)},status={escape_tag(status)}"
            fields = (
                f"unpaid={unpaid},"
                f"profitability={profitability},"
                f"speed_accepted={speed_accepted},"
                f"speed_rejected={speed_rejected}"
            )
            lines.append(f"nicehash_rigs,{tags} {fields} {now_ns}")

    except Exception as e:
        print(f"ERROR fetching rigs: {e}", file=sys.stderr)

    return lines


def fetch_balance(cfg):
    """Fetch account balances and output as InfluxDB line protocol."""
    lines = []
    try:
        data = nicehash_request(cfg, "GET", "/main/api/v2/accounting/accounts2/", "")
        now_ns = int(time.time() * 1e9)

        currencies = data.get("currencies", data.get("total", data)) if isinstance(data, dict) else data

        # Handle different response formats
        if isinstance(data, dict) and "currencies" in data:
            for acc in data["currencies"]:
                currency = acc.get("currency", "UNKNOWN")
                available = float(acc.get("available", 0))
                pending = float(acc.get("pending", 0))
                total = available + pending
                if total == 0:
                    continue
                tags = f"currency={escape_tag(currency)}"
                fields = f"available={available},pending={pending},total={total}"
                lines.append(f"nicehash_balance,{tags} {fields} {now_ns}")
        elif isinstance(data, dict) and "total" in data:
            # Alternative response format
            for currency, balances in data["total"].items():
                available = float(balances.get("available", 0))
                pending = float(balances.get("pending", 0))
                total = available + pending
                if total == 0:
                    continue
                tags = f"currency={escape_tag(currency)}"
                fields = f"available={available},pending={pending},total={total}"
                lines.append(f"nicehash_balance,{tags} {fields} {now_ns}")

    except Exception as e:
        print(f"ERROR fetching balance: {e}", file=sys.stderr)

    return lines


def main():
    parser = argparse.ArgumentParser(description="NiceHash API poller for Telegraf")
    parser.add_argument(
        "--config",
        default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "config.json"),
        help="Path to NiceHash config JSON file (default: config.json next to script)",
    )
    args = parser.parse_args()

    cfg = load_config(args.config)

    lines = []
    lines.extend(fetch_rigs(cfg))
    lines.extend(fetch_payouts(cfg))
    lines.extend(fetch_balance(cfg))

    for line in lines:
        print(line)


if __name__ == "__main__":
    main()
