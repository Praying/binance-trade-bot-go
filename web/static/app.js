document.addEventListener('DOMContentLoaded', function() {
    const statusElement = document.getElementById('current-coin');
    const tradesBody = document.getElementById('trades-body');

    const fetchStatus = async () => {
        try {
            const response = await fetch('/api/status');
            if (!response.ok) {
                throw new Error('Network response was not ok');
            }
            const data = await response.json();
            statusElement.textContent = data.Symbol || 'N/A';
        } catch (error) {
            console.error('Failed to fetch status:', error);
            statusElement.textContent = 'Error loading status';
        }
    };

    const fetchTrades = async () => {
        try {
            const response = await fetch('/api/trades');
            if (!response.ok) {
                throw new Error('Network response was not ok');
            }
            const trades = await response.json();
            renderTrades(trades);
        } catch (error) {
            console.error('Failed to fetch trades:', error);
            tradesBody.innerHTML = '<tr><td colspan="7">Error loading trades.</td></tr>';
        }
    };

    const renderTrades = (trades) => {
        if (!trades || trades.length === 0) {
            tradesBody.innerHTML = '<tr><td colspan="7">No trades found.</td></tr>';
            return;
        }

        // Clear existing rows
        tradesBody.innerHTML = '';

        trades.forEach(trade => {
            const row = document.createElement('tr');
            
            const tradeTime = new Date(trade.Timestamp * 1000).toLocaleString();
            const sideClass = trade.Type.toLowerCase(); // 'buy' or 'sell'
            const price = trade.Price.toFixed(4);
            const quantity = trade.Quantity.toFixed(6);
            const total = trade.QuoteQuantity.toFixed(4);
            const simulation = trade.IsSimulation ? 'Yes' : 'No';

            row.innerHTML = `
                <td>${tradeTime}</td>
                <td>${trade.Symbol}</td>
                <td class="${sideClass}">${trade.Type}</td>
                <td>${price}</td>
                <td>${quantity}</td>
                <td>${total}</td>
                <td>${simulation}</td>
            `;
            tradesBody.appendChild(row);
        });
    };

    // Initial fetch
    fetchStatus();
    fetchTrades();

    // Fetch data every 5 seconds
    setInterval(() => {
        fetchStatus();
        fetchTrades();
    }, 5000);
});