package tradeview

import "context"

// OpenStreamState opens the typed newline-delimited stream for state.
func (client *Client) OpenStreamState(ctx context.Context, accountID PathAccountId, params *StreamStateParams, options ...StreamOption) (*Stream[StateEvent], error) {
	ctx = typedTelemetryContext(ctx)
	config, err := configureStream(options)
	if err != nil {
		return nil, err
	}
	response, err := client.StreamState(ctx, accountID, params, config.requestEditors...)
	return openHTTPStream(ctx, response, err, config, decodeAccountEvent[AccountStateMessage])
}

// OpenStreamOrders opens the typed newline-delimited stream for orders.
func (client *Client) OpenStreamOrders(ctx context.Context, accountID PathAccountId, params *StreamOrdersParams, options ...StreamOption) (*Stream[OrdersEvent], error) {
	ctx = typedTelemetryContext(ctx)
	config, err := configureStream(options)
	if err != nil {
		return nil, err
	}
	response, err := client.StreamOrders(ctx, accountID, params, config.requestEditors...)
	return openHTTPStream(ctx, response, err, config, decodeAccountEvent[OrdersMessage])
}

// OpenStreamPositions opens the typed newline-delimited stream for positions.
func (client *Client) OpenStreamPositions(ctx context.Context, accountID PathAccountId, params *StreamPositionsParams, options ...StreamOption) (*Stream[PositionsEvent], error) {
	ctx = typedTelemetryContext(ctx)
	config, err := configureStream(options)
	if err != nil {
		return nil, err
	}
	response, err := client.StreamPositions(ctx, accountID, params, config.requestEditors...)
	return openHTTPStream(ctx, response, err, config, decodeAccountEvent[StreamPositionsMessage])
}

// OpenStreamNetPositions opens the typed newline-delimited stream for netPositions.
func (client *Client) OpenStreamNetPositions(ctx context.Context, accountID PathAccountId, params *StreamNetPositionsParams, options ...StreamOption) (*Stream[NetPositionsEvent], error) {
	ctx = typedTelemetryContext(ctx)
	config, err := configureStream(options)
	if err != nil {
		return nil, err
	}
	response, err := client.StreamNetPositions(ctx, accountID, params, config.requestEditors...)
	return openHTTPStream(ctx, response, err, config, decodeAccountEvent[StreamNetPositionsMessage])
}

// OpenStreamIndividualPositions opens the typed newline-delimited stream for individualPositions.
func (client *Client) OpenStreamIndividualPositions(ctx context.Context, accountID PathAccountId, params *StreamIndividualPositionsParams, options ...StreamOption) (*Stream[IndividualPositionsEvent], error) {
	ctx = typedTelemetryContext(ctx)
	config, err := configureStream(options)
	if err != nil {
		return nil, err
	}
	response, err := client.StreamIndividualPositions(ctx, accountID, params, config.requestEditors...)
	return openHTTPStream(ctx, response, err, config, decodeAccountEvent[StreamIndividualPositionsMessage])
}

// OpenStreamNotifications opens the typed newline-delimited stream for notifications.
func (client *Client) OpenStreamNotifications(ctx context.Context, accountID PathAccountId, params *StreamNotificationsParams, options ...StreamOption) (*Stream[NotificationsEvent], error) {
	ctx = typedTelemetryContext(ctx)
	config, err := configureStream(options)
	if err != nil {
		return nil, err
	}
	response, err := client.StreamNotifications(ctx, accountID, params, config.requestEditors...)
	return openHTTPStream(ctx, response, err, config, decodeAccountEvent[StreamNotificationsMessage])
}

// OpenStreamQuotes opens the typed newline-delimited stream for quotes.
func (client *Client) OpenStreamQuotes(ctx context.Context, params *StreamQuotesParams, options ...StreamOption) (*Stream[QuotesEvent], error) {
	ctx = typedTelemetryContext(ctx)
	config, err := configureStream(options)
	if err != nil {
		return nil, err
	}
	response, err := client.StreamQuotes(ctx, params, config.requestEditors...)
	return openHTTPStream(ctx, response, err, config, decodeAccountEvent[QuotesMessage])
}

// OpenStreamDepth opens the typed newline-delimited stream for depth.
func (client *Client) OpenStreamDepth(ctx context.Context, params *StreamDepthParams, options ...StreamOption) (*Stream[DepthEvent], error) {
	ctx = typedTelemetryContext(ctx)
	config, err := configureStream(options)
	if err != nil {
		return nil, err
	}
	response, err := client.StreamDepth(ctx, params, config.requestEditors...)
	return openHTTPStream(ctx, response, err, config, decodeAccountEvent[Depth])
}

// OpenStreaming opens the typed /streaming market-data feed.
func (client *Client) OpenStreaming(ctx context.Context, options ...StreamOption) (*Stream[MarketEvent], error) {
	ctx = typedTelemetryContext(ctx)
	config, err := configureStream(options)
	if err != nil {
		return nil, err
	}
	response, err := client.Streaming(ctx, config.requestEditors...)
	return openHTTPStream(ctx, response, err, config, decodeMarketEvent)
}
