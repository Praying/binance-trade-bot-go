document.addEventListener('DOMContentLoaded', function() {
    const tradersBody = document.getElementById('traders-body');
    const tradesBody = document.getElementById('trades-body');
   
    const stats24h = {
    	total: document.getElementById('stats-24h-total'),
    	profitable: document.getElementById('stats-24h-profitable'),
    	winrate: document.getElementById('stats-24h-winrate'),
    	profit: document.getElementById('stats-24h-profit'),
    };
   
    const statsAll = {
    	total: document.getElementById('stats-all-total'),
    	profitable: document.getElementById('stats-all-profitable'),
    	winrate: document.getElementById('stats-all-winrate'),
    	profit: document.getElementById('stats-all-profit'),
    };

    const fetchTraders = async () => {
        try {
            const response = await fetch('/api/traders');
            if (!response.ok) {
                throw new Error('Network response was not ok');
            }
            const traders = await response.json();
            renderTraders(traders);
        } catch (error) {
            console.error('Failed to fetch traders:', error);
            tradersBody.innerHTML = '<tr><td colspan="5">Error loading traders.</td></tr>';
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
            tradesBody.innerHTML = '<tr><td colspan="8">Error loading trades.</td></tr>';
        }
    };
   
    const fetchStatistics = async () => {
    	try {
    		const response = await fetch('/api/statistics');
    		if (!response.ok) {
    			throw new Error('Network response was not ok');
    		}
    		const data = await response.json();
    		renderStatistics(data);
    	} catch (error) {
    		console.error('Failed to fetch statistics:', error);
    		// Optionally, display an error in the UI
    	}
    };
    
    const renderStatistics = (data) => {
    	const { since_24h, all_time } = data;
   
    	stats24h.total.textContent = since_24h.total_trades;
    	stats24h.profitable.textContent = since_24h.profitable_trades;
    	stats24h.winrate.textContent = (since_24h.win_rate * 100).toFixed(2) + '%';
    	stats24h.profit.textContent = since_24h.total_profit.toFixed(4);
   
    	statsAll.total.textContent = all_time.total_trades;
    	statsAll.profitable.textContent = all_time.profitable_trades;
    	statsAll.winrate.textContent = (all_time.win_rate * 100).toFixed(2) + '%';
    	statsAll.profit.textContent = all_time.total_profit.toFixed(4);
    };

    const renderTrades = (trades) => {
        if (!trades || trades.length === 0) {
            tradesBody.innerHTML = `<tr><td colspan="8">No trades found.</td></tr>`;
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
            const profit = trade.profit ? trade.profit.toFixed(4) : 'N/A';
            const simulation = trade.IsSimulation ? 'Yes' : 'No';

            row.innerHTML = `
                <td>${tradeTime}</td>
                <td>${trade.Symbol}</td>
                <td class="${sideClass}">${trade.Type}</td>
                <td>${price}</td>
                <td>${quantity}</td>
                <td>${total}</td>
                <td>${profit}</td>
                <td>${simulation}</td>
            `;
            tradesBody.appendChild(row);
        });
    };

    const renderTraders = (traders) => {
        if (!traders || traders.length === 0) {
            tradersBody.innerHTML = `<tr><td colspan="5">No trader instances found.</td></tr>`;
            return;
        }

        tradersBody.innerHTML = '';

        traders.forEach(trader => {
            const row = document.createElement('tr');
            const statusClass = trader.is_healthy ? 'status-healthy' : 'status-unhealthy';
            const statusText = trader.is_healthy ? 'Healthy' : 'Unhealthy';

            row.innerHTML = `
                <td>${trader.name}</td>
                <td>${trader.uuid}</td>
                <td>${trader.strategy}</td>
                <td>${trader.uptime}</td>
                <td class="${statusClass}">${statusText}</td>
            `;
            tradersBody.appendChild(row);
        });
    };

    // Initial fetch
    fetchTraders();
    fetchTrades();
    fetchStatistics();

    // Fetch data every 5 seconds
    setInterval(() => {
        fetchTraders();
        fetchTrades();
        fetchStatistics();
    }, 5000);
});