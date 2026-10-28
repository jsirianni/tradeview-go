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

[Usage](docs/usage.md) | [Logging](docs/logging.md) | [OpenTelemetry](docs/telemetry.md) | [Development](docs/development.md)
