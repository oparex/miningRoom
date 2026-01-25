package db

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

type Machine struct {
	Name string
	IP   string
}

type DB struct {
	conn *sql.DB
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	return &DB{conn: conn}, nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) EnsureSchema() error {
	_, err := d.conn.Exec(`
		CREATE TABLE IF NOT EXISTS machines (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			ip TEXT NOT NULL
		)
	`)
	return err
}

func (d *DB) FetchMachines() ([]Machine, error) {
	rows, err := d.conn.Query("SELECT name, ip FROM machines ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var machines []Machine
	for rows.Next() {
		var m Machine
		if err := rows.Scan(&m.Name, &m.IP); err != nil {
			return nil, err
		}
		machines = append(machines, m)
	}
	return machines, rows.Err()
}

func (d *DB) AddMachine(name, ip string) error {
	_, err := d.conn.Exec("INSERT INTO machines (name, ip) VALUES (?, ?)", name, ip)
	return err
}

func (d *DB) DeleteMachine(ip string) error {
	_, err := d.conn.Exec("DELETE FROM machines WHERE ip = ?", ip)
	return err
}
