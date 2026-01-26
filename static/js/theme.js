// Theme management
(function() {
    const THEME_KEY = 'dashboard-theme';

    // Get saved theme or default to light
    function getTheme() {
        return localStorage.getItem(THEME_KEY) || 'light';
    }

    // Save theme preference
    function setTheme(theme) {
        localStorage.setItem(THEME_KEY, theme);
        applyTheme(theme);
    }

    // Apply theme to document
    function applyTheme(theme) {
        document.documentElement.setAttribute('data-theme', theme);

        // Update any theme toggle switches on the page
        const toggles = document.querySelectorAll('#darkModeToggle');
        toggles.forEach(toggle => {
            toggle.checked = theme === 'dark';
        });

        // Update Chart.js colors if charts exist
        updateChartColors(theme);
    }

    // Update Chart.js colors for dark mode
    function updateChartColors(theme) {
        if (typeof Chart === 'undefined') return;

        const textColor = theme === 'dark' ? '#e9ecef' : '#666';
        const gridColor = theme === 'dark' ? 'rgba(255, 255, 255, 0.1)' : 'rgba(0, 0, 0, 0.1)';

        Chart.defaults.color = textColor;
        Chart.defaults.borderColor = gridColor;

        // Update existing charts if they exist
        if (window.dashboardCharts) {
            Object.keys(window.dashboardCharts).forEach(key => {
                const chart = window.dashboardCharts[key];
                if (chart && chart.update && typeof chart.update === 'function') {
                    chart.update();
                }
            });
        }

        // Update any Chart instances
        if (Chart.instances) {
            Object.values(Chart.instances).forEach(chart => {
                if (chart && chart.update) {
                    chart.update();
                }
            });
        }
    }

    // Toggle between light and dark
    function toggleTheme() {
        const currentTheme = getTheme();
        const newTheme = currentTheme === 'dark' ? 'light' : 'dark';
        setTheme(newTheme);
        return newTheme;
    }

    // Initialize theme on page load
    function initTheme() {
        const savedTheme = getTheme();
        applyTheme(savedTheme);
    }

    // Apply theme immediately to prevent flash
    initTheme();

    // Re-apply after DOM is fully loaded
    document.addEventListener('DOMContentLoaded', function() {
        initTheme();

        // Set up toggle listeners
        const toggles = document.querySelectorAll('#darkModeToggle');
        toggles.forEach(toggle => {
            toggle.addEventListener('change', function() {
                setTheme(this.checked ? 'dark' : 'light');
            });
        });
    });

    // Export for global access
    window.themeManager = {
        getTheme: getTheme,
        setTheme: setTheme,
        toggleTheme: toggleTheme
    };
})();
