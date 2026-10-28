// Package tradeview provides HTTP client bindings for the TradingView broker REST API.
//
// Clients connect to a caller-supplied broker base URL. The package covers the pinned
// broker contract, including optional endpoints and HTTP streaming; it does not
// provide access to a general TradingView consumer market-data service.
//
// Use NewClientWithResponses for typed finite responses and NewClient with the
// OpenStream methods for incremental streams. Callers own token acquisition,
// deadlines, retries, reconnection, pagination traversal, and application state.
package tradeview
