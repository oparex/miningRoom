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

// Chart configuration
const chartColors = {
    primary: 'rgba(13, 110, 253, 1)',
    primaryLight: 'rgba(13, 110, 253, 0.2)',
    success: 'rgba(25, 135, 84, 1)',
    successLight: 'rgba(25, 135, 84, 0.2)',
    warning: 'rgba(255, 193, 7, 1)',
    warningLight: 'rgba(255, 193, 7, 0.2)',
    danger: 'rgba(220, 53, 69, 1)',
    dangerLight: 'rgba(220, 53, 69, 0.2)',
    info: 'rgba(13, 202, 240, 1)',
    infoLight: 'rgba(13, 202, 240, 0.2)',
    purple: 'rgba(111, 66, 193, 1)',
    purpleLight: 'rgba(111, 66, 193, 0.2)',
};

// Color palette for dynamic datasets
const colorPalette = [
    { border: chartColors.primary, background: chartColors.primaryLight },
    { border: chartColors.success, background: chartColors.successLight },
    { border: chartColors.warning, background: chartColors.warningLight },
    { border: chartColors.danger, background: chartColors.dangerLight },
    { border: chartColors.info, background: chartColors.infoLight },
    { border: chartColors.purple, background: chartColors.purpleLight },
];

// Sample labels for charts (will be replaced with real data)
const sampleLabels = ['00:00', '04:00', '08:00', '12:00', '16:00', '20:00', '24:00'];

// Chart 1 - Environment Temperature Chart
const chart1Ctx = document.getElementById('chart1').getContext('2d');
let chart1 = new Chart(chart1Ctx, {
    type: 'line',
    data: {
        datasets: []
    },
    options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
            legend: {
                display: false
            },
            title: {
                display: false
            }
        },
        scales: {
            x: {
                type: 'time',
                display: false
            },
            y: {
                title: {
                    display: true,
                    text: '°C'
                },
                grace: '10%'
            }
        }
    }
});

// Fetch and update environment temperature chart
async function loadEnvironmentChart() {
    console.log('Loading environment chart data...');
    try {
        const response = await fetch('/api/charts/environment');
        console.log('Response status:', response.status);
        const data = await response.json();
        console.log('Environment data:', data);

        if (!data.hasData || !data.locations) {
            console.log('No environment data available');
            return;
        }

        const datasets = [];
        let colorIndex = 0;

        for (const [location, readings] of Object.entries(data.locations)) {
            console.log(`Processing location: ${location}, readings: ${readings.length}`);
            const color = colorPalette[colorIndex % colorPalette.length];

            datasets.push({
                label: location,
                data: readings.map(r => ({
                    x: new Date(r.timestamp),
                    y: r.temperature
                })),
                borderColor: color.border,
                backgroundColor: color.background,
                fill: false,
                tension: 0.4,
                pointRadius: 0,
                borderWidth: 2
            });

            colorIndex++;
        }

        console.log('Created datasets:', datasets.length);
        chart1.data.datasets = datasets;
        chart1.update();
        console.log('Chart updated');
    } catch (error) {
        console.error('Failed to load environment chart:', error);
    }
}

// Load environment chart on page load
loadEnvironmentChart();

// Refresh environment chart every 5 minutes
setInterval(loadEnvironmentChart, 5 * 60 * 1000);

// Chart 2 - Bar Chart (placeholder)
const chart2Ctx = document.getElementById('chart2').getContext('2d');
const chart2 = new Chart(chart2Ctx, {
    type: 'bar',
    data: {
        labels: sampleLabels,
        datasets: [{
            label: 'Dataset 2',
            data: [45, 67, 34, 78, 52, 89, 63],
            backgroundColor: chartColors.success,
            borderColor: chartColors.success,
            borderWidth: 1
        }]
    },
    options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
            legend: {
                display: true,
                position: 'top'
            }
        },
        scales: {
            y: {
                beginAtZero: true
            }
        }
    }
});

// Chart 3 - Line Chart (placeholder)
const chart3Ctx = document.getElementById('chart3').getContext('2d');
const chart3 = new Chart(chart3Ctx, {
    type: 'line',
    data: {
        labels: sampleLabels,
        datasets: [{
            label: 'Dataset 3',
            data: [28, 48, 40, 19, 86, 27, 55],
            borderColor: chartColors.warning,
            backgroundColor: chartColors.warningLight,
            fill: true,
            tension: 0.4
        }]
    },
    options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
            legend: {
                display: true,
                position: 'top'
            }
        },
        scales: {
            y: {
                beginAtZero: true
            }
        }
    }
});

// Chart 4 - Doughnut Chart (placeholder)
const chart4Ctx = document.getElementById('chart4').getContext('2d');
const chart4 = new Chart(chart4Ctx, {
    type: 'doughnut',
    data: {
        labels: ['Category A', 'Category B', 'Category C', 'Category D'],
        datasets: [{
            data: [30, 25, 20, 25],
            backgroundColor: [
                chartColors.primary,
                chartColors.success,
                chartColors.warning,
                chartColors.danger
            ],
            borderWidth: 0
        }]
    },
    options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
            legend: {
                display: true,
                position: 'right'
            }
        }
    }
});

// Function to update charts with new data (to be called from API responses)
function updateChart(chartInstance, labels, data) {
    chartInstance.data.labels = labels;
    chartInstance.data.datasets[0].data = data;
    chartInstance.update();
}

// Export chart instances for external use
window.dashboardCharts = {
    chart1: chart1,
    chart2: chart2,
    chart3: chart3,
    chart4: chart4,
    updateChart: updateChart
};
