// Sidebar toggle
document.getElementById('sidebarCollapse').addEventListener('click', function () {
    document.getElementById('sidebar').classList.toggle('active');
});

// Update current time
function updateTime() {
    const now = new Date();
    const timeString = now.toLocaleTimeString();
    document.getElementById('currentTime').textContent = timeString;
}
updateTime();
setInterval(updateTime, 1000);

// Environment Temperature Boxes
async function loadEnvironmentTemps() {
    try {
        const response = await fetch('/api/environment/latest');
        const data = await response.json();
        const container = document.getElementById('envTempContainer');

        if (!data.hasData || !data.readings || data.readings.length === 0) {
            container.innerHTML = '<div class="col"><div class="text-center text-muted py-5">No environment data available</div></div>';
            return;
        }

        const locationOrder = ['outside', 'miningroom', 'washroom'];
        data.readings.sort((a, b) => {
            const ia = locationOrder.indexOf(a.location);
            const ib = locationOrder.indexOf(b.location);
            return (ia === -1 ? 999 : ia) - (ib === -1 ? 999 : ib);
        });

        container.innerHTML = data.readings.map(r => {
            const ts = new Date(r.timestamp.endsWith('Z') ? r.timestamp : r.timestamp + 'Z');
            const ageMs = Date.now() - ts.getTime();
            const stale = isNaN(ageMs) || ageMs > 120 * 1000;
            const bgClass = stale ? 'bg-danger text-white' : 'bg-light';
            const textClass = stale ? 'text-white' : 'text-primary';
            const mutedClass = stale ? 'text-white-50' : 'text-muted';
            const temp = r.temperature.toFixed(1);
            return `<div class="col-lg-4 col-md-4 mb-3 mb-lg-0">
                <div class="text-center p-3 rounded ${bgClass} h-100 d-flex flex-column justify-content-center" style="min-height: 180px;">
                    <div class="${mutedClass} small mb-1">${r.location}</div>
                    <div class="h2 mb-0 ${textClass}">${temp}</div>
                    <div class="${mutedClass} small">&deg;C</div>
                </div>
            </div>`;
        }).join('');
    } catch (error) {
        console.error('Failed to load environment temperatures:', error);
    }
}

loadEnvironmentTemps();
setInterval(loadEnvironmentTemps, 60 * 1000);

// Miner Status Table
async function loadMinerStatus() {
    try {
        const response = await fetch('/api/miners/status');
        const data = await response.json();
        const tbody = document.getElementById('minerStatusBody');

        if (!data.hasData || !data.miners || data.miners.length === 0) {
            tbody.innerHTML = '<tr><td colspan="8" class="text-center text-muted py-3">No miner data available</td></tr>';
            return;
        }

        tbody.innerHTML = data.miners.map(m => {
            const statusLower = m.status.toLowerCase();
            const statusClass = statusLower === 'mining' ? 'bg-success' : statusLower === 'initializing' ? 'bg-warning' : 'bg-secondary';
            const hashrateTH = (m.hashrate / 1000).toFixed(1);
            const power = Math.round(m.power);
            const efficiency = m.efficiency.toFixed(1);
            const temp = m.temperatureMax.toFixed(1);
            return `<tr>
                <td class="fw-semibold">${m.name}</td>
                <td><code>${m.minerIp}</code></td>
                <td><span class="badge ${statusClass}">${m.status}</span></td>
                <td>${m.workMode}</td>
                <td>${hashrateTH} TH/s</td>
                <td>${power} W</td>
                <td>${efficiency} J/TH</td>
                <td>${temp} &deg;C</td>
            </tr>`;
        }).join('');
    } catch (error) {
        console.error('Failed to load miner status:', error);
    }
}

loadMinerStatus();
setInterval(loadMinerStatus, 60 * 1000);
