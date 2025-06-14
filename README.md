# Go Binance Trading Bot

[![Build Status](https://github.com/Praying/binance-trade-bot-go/actions/workflows/go.yml/badge.svg)](https://github.com/Praying/binance-trade-bot-go/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Praying/binance-trade-bot-go)](https://goreportcard.com/report/github.com/Praying/binance-trade-bot-go)
[![Go Version](https://img.shields.io/badge/go-1.18%2B-blue.svg)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Disclaimer:** This is a personal project and should not be used in a production environment. Cryptocurrency trading involves substantial risk of loss and is not suitable for every investor. All trading decisions are your own, and you are solely responsible for any losses that may occur.

This project is a high-performance cryptocurrency trading bot for Binance, rewritten in Go from an original Python implementation. It employs a triangular arbitrage strategy and has been significantly enhanced with modern, production-ready features.

## ✨ Features

- **High-Performance Trading Engine**: Utilizes Go's concurrency to scout for trading opportunities across multiple pairs simultaneously, offering a significant performance advantage over sequential logic.
- **Resilient API Client**:
    - **Global Rate Limiting**: Proactively manages request rates to stay within Binance's API limits.
    - **Smart Retries & Exponential Backoff**: Automatically retries on network/server errors and intelligently waits when rate-limited (respecting `Retry-After` headers).
- **Accurate Profit Calculation**: Trading fees are factored into all profit calculations to reflect real-world outcomes.
- **Reliable Order Placement**: Automatically formats order quantities to comply with Binance's `LOT_SIZE` rules, preventing rejections due to precision errors.
- **Web Interface**: A clean, real-time web dashboard to monitor the bot's current holdings and view detailed trade history.

  *(screenshot placeholder)*
- **Testnet Support**: Easily switch between Binance's production and testnet environments via a simple configuration flag, allowing for safe testing.
- **Persistent Trade History**: Every trade (both simulated and real) is recorded in a local SQLite database for analysis and auditing.
- **Modern Stack**: Built with a robust stack including `Viper` for configuration, `Zap` for structured logging, and `GORM` for database interactions.

## 🛠️ Tech Stack

- **Language**: Go
- **Configuration**: [Viper](https://github.com/spf13/viper)
- **Logging**: [Zap](https://github.com/uber-go/zap)
- **Database**: [GORM](https://gorm.io/) with SQLite
- **HTTP Client**: [Resty](https://github.com/go-resty/resty)
- **API Rate Limiting**: `golang.org/x/time/rate`

## 📂 Directory Structure

```
.
├── cmd/                # Main applications
│   ├── trader/         # The core trading bot application
│   └── ui/             # The web interface server
├── configs/            # Configuration files
│   └── config.example.yml
├── internal/           # Private application logic
│   ├── binance/        # Binance API client
│   ├── config/         # Configuration loading
│   ├── database/       # Database setup and migration
│   ├── logger/         # Logger setup
│   ├── models/         # GORM database models
│   └── trader/         # Core trading strategy and engine
├── web/                # Frontend files for the UI
│   ├── static/         # CSS and JS files
│   └── templates/      # HTML templates
└── trades.db           # SQLite database file (auto-generated)
```

## 🚀 Getting Started

### 1. Prerequisites

- Go 1.18 or higher.

### 2. Configuration

1.  **Copy the example config**:
    ```bash
    cp configs/config.example.yml configs/config.yml
    ```

2.  **Edit `configs/config.yml`**:
    -   **`binance` section**:
        -   Set your `apiKey` and `secretKey`. For better security, it's highly recommended to use environment variables instead:
            ```bash
            export BINANCE_APIKEY=your_api_key
            export BINANCE_SECRETKEY=your_secret_key
            ```
        -   Set `testnet` to `true` for testing or `false` for real trading.
    -   **`trading` section**:
        -   Configure your `bridge` currency (e.g., "USDT").
        -   List the `trade_pairs` you want the bot to monitor (e.g., "BTC", "ETH").
        -   Set `dry_run` to `true` to simulate trades without executing them on the exchange.

### 3. Running the Bot

You need to run two services in separate terminals.

-   **Terminal 1: Start the Trading Engine**
    ```bash
    go run cmd/trader/main.go
    ```
    This will start the bot, and it will begin scouting for trades based on your configuration.

-   **Terminal 2: Start the Web UI Server**
    ```bash
    go run cmd/ui/main.go
    ```
    This launches the web server that provides the monitoring dashboard.

### 4. Accessing the Web UI

Once both services are running, open your web browser and navigate to:

**[http://localhost:8080](http://localhost:8080)**

You will see a dashboard displaying the bot's current status and a table with all historical trades, which updates automatically.

## 📈 Trading Strategy

The bot uses a simple triangular arbitrage strategy with a "bridge" currency (e.g., USDT).

1.  **Start State**: The bot holds the bridge currency (USDT).
2.  **Scout for Buys**: It continuously monitors the prices of all configured `trade_pairs` (e.g., BTC/USDT, ETH/USDT).
3.  **Buy Condition**: If the current price of a coin (e.g., BTC) drops below its recorded "base ratio", the bot sees a profitable opportunity to **BUY** that coin with USDT.
4.  **Hold State**: After buying, the bot holds the new coin (e.g., BTC).
5.  **Sell Condition**: It now waits for the coin's price to rise back above the "base ratio". Once it does, it **SELLS** the coin, converting it back to the bridge currency (USDT) and realizing a profit.
6.  The cycle repeats.

## 🤝 Contributing

Contributions are welcome! If you'd like to help improve the project, please feel free to:

- Report a bug via [GitHub Issues](https://github.com/Praying/binance-trade-bot-go/issues).
- Suggest a new feature.
- Submit a pull request.

We have a simple pull request process. Please fork the repository, create a new branch for your feature or fix, and submit a PR.

## 📜 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.