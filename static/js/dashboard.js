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
};

// Sample labels for charts (will be replaced with real data)
const sampleLabels = ['00:00', '04:00', '08:00', '12:00', '16:00', '20:00', '24:00'];

// Chart 1 - Line Chart (placeholder)
const chart1Ctx = document.getElementById('chart1').getContext('2d');
const chart1 = new Chart(chart1Ctx, {
    type: 'line',
    data: {
        labels: sampleLabels,
        datasets: [{
            label: 'Dataset 1',
            data: [65, 59, 80, 81, 56, 55, 72],
            borderColor: chartColors.primary,
            backgroundColor: chartColors.primaryLight,
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
