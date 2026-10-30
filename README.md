# tradeview-go

A Go client for the official TradingView broker REST API, connecting applications to broker-provided endpoints.

## Features

- Complete coverage of all 47 operations in API version 1.21.0.
- Accounts, balances, orders, positions, executions, and trading history.
- Symbol information, historical prices, quotes, and market depth.
- Nine live HTTP streams for account activity and market data.
- Typed requests, responses, and API errors.
- Bearer authentication, configurable HTTP clients, request cancellation, and custom order fields.
- Optional OpenTelemetry tracing and metrics across finite requests and live streams.
- Optional structured logging to stdout or files with standard Go `slog`.
- A provider-neutral alert interface with Slack bot delivery.

[Usage](docs/usage.md) | [Alerts](docs/alerts.md) | [Logging](docs/logging.md) | [OpenTelemetry](docs/telemetry.md) | [Development](docs/development.md)

## License

This project is licensed under the [MIT License](LICENSE).

## Development disclosure and disclaimer

This project is primarily developed using AI-assisted coding tools.

The software is provided "as is", without warranty or guarantee of any kind, express or implied. No guarantee is made regarding its accuracy, reliability, security, or fitness for any particular purpose. Use it at your own risk. See the [MIT License](LICENSE) for the full warranty disclaimer and limitation of liability.
