package tradeview

import (
	"context"
	"net/url"
)

func finiteResponseOperations() []responseOperation {
	return []responseOperation{
		{id: "authorize", request: responseRequest{method: "POST", path: "/authorize", query: url.Values{}, form: url.Values{"login": {"fixture-user"}, "password": {"fixture-password"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.AuthorizeWithFormdataBodyWithResponse(ctx, AuthorizeFormdataRequestBody{Login: "fixture-user", Password: "fixture-password"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "AuthorizeResponse", file: "authorize", check: checkResponseUnion(Authorize200JSONResponseBody.AsAuthorizePayload, assertAuthorize)},
			}},
		{id: "cancelAllOrders", request: responseRequest{method: "POST", path: "/accounts/account-1/orders/cancelAll", query: url.Values{"locale": {"en"}}, form: url.Values{"symbol": {"EURUSD"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				var body CancelAllOrdersFormdataRequestBody
				if err := body.FromCancelAllOrdersBySymbolRequestBody(CancelAllOrdersBySymbolRequestBody{Symbol: "EURUSD"}); err != nil {
					return responseObservation{}, err
				}
				r, err := c.CancelAllOrdersWithFormdataBodyWithResponse(ctx, "account-1", &CancelAllOrdersParams{Locale: "en"}, body)
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "SuccessResponse", file: "success", check: checkResponseUnion(CancelAllOrders200JSONResponseBody.AsSuccessResponse, assertSuccess)},
			}},
		{id: "cancelOrder", request: responseRequest{method: "DELETE", path: "/accounts/account-1/orders/order-1", query: url.Values{"locale": {"en"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.CancelOrderWithResponse(ctx, "account-1", "order-1", &CancelOrderParams{Locale: "en"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "SuccessResponse", file: "success", check: checkResponseUnion(CancelOrder200JSONResponseBody.AsSuccessResponse, assertSuccess)},
				{schema: "SuccessResponseWithTransactionId", file: "transaction", check: checkResponseUnion(CancelOrder200JSONResponseBody.AsSuccessResponseWithTransactionId, assertTransaction)},
			}},
		{id: "closeIndividualPosition", request: responseRequest{method: "DELETE", path: "/accounts/account-1/individualPositions/position-1", query: url.Values{"locale": {"en"}}, form: url.Values{"amount": {"2.5"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.CloseIndividualPositionWithFormdataBodyWithResponse(ctx, "account-1", "position-1", &CloseIndividualPositionParams{Locale: "en"}, CloseIndividualPositionFormdataRequestBody{Amount: new(2.5)})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "SuccessResponse", file: "success", check: checkResponseUnion(CloseIndividualPosition200JSONResponseBody.AsSuccessResponse, assertSuccess)},
				{schema: "SuccessResponseWithTransactionId", file: "transaction", check: checkResponseUnion(CloseIndividualPosition200JSONResponseBody.AsSuccessResponseWithTransactionId, assertTransaction)},
			}},
		{id: "closeNetPosition", request: responseRequest{method: "DELETE", path: "/accounts/account-1/netPositions/position-1", query: url.Values{"locale": {"en"}}, form: url.Values{"amount": {"2.5"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.CloseNetPositionWithFormdataBodyWithResponse(ctx, "account-1", "position-1", &CloseNetPositionParams{Locale: "en"}, CloseNetPositionFormdataRequestBody{Amount: new(2.5)})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "SuccessResponse", file: "success", check: checkResponseUnion(CloseNetPosition200JSONResponseBody.AsSuccessResponse, assertSuccess)},
				{schema: "SuccessResponseWithTransactionId", file: "transaction", check: checkResponseUnion(CloseNetPosition200JSONResponseBody.AsSuccessResponseWithTransactionId, assertTransaction)},
			}},
		{id: "closePosition", request: responseRequest{method: "DELETE", path: "/accounts/account-1/positions/position-1", query: url.Values{"locale": {"en"}}, form: url.Values{"amount": {"2.5"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.ClosePositionWithFormdataBodyWithResponse(ctx, "account-1", "position-1", &ClosePositionParams{Locale: "en"}, ClosePositionFormdataRequestBody{Amount: new(2.5)})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "SuccessResponse", file: "success", check: checkResponseUnion(ClosePosition200JSONResponseBody.AsSuccessResponse, assertSuccess)},
				{schema: "SuccessResponseWithTransactionId", file: "transaction", check: checkResponseUnion(ClosePosition200JSONResponseBody.AsSuccessResponseWithTransactionId, assertTransaction)},
			}},
		{id: "getAccounts", request: responseRequest{method: "GET", path: "/accounts", query: url.Values{"locale": {"en"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetAccountsWithResponse(ctx, &GetAccountsParams{Locale: "en"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "AccountResponse", file: "accounts", check: checkResponseUnion(GetAccounts200JSONResponseBody.AsAccountResponse, assertAccounts)},
			}},
		{id: "getBalances", request: responseRequest{method: "GET", path: "/accounts/account-1/balances", query: url.Values{"locale": {"en"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetBalancesWithResponse(ctx, "account-1", &GetBalancesParams{Locale: "en"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "CryptoBalancesResponse", file: "balances", check: checkResponseUnion(GetBalances200JSONResponseBody.AsCryptoBalancesResponse, assertBalances)},
			}},
		{id: "getConfiguration", request: responseRequest{method: "GET", path: "/config", query: url.Values{"locale": {"en"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetConfigurationWithResponse(ctx, &GetConfigurationParams{Locale: "en"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "ConfigResponse", file: "configuration", check: checkResponseUnion(GetConfiguration200JSONResponseBody.AsConfigResponse, assertConfiguration)},
			}},
		{id: "getDepth", request: responseRequest{method: "GET", path: "/depth", query: url.Values{"locale": {"en"}, "accountId": {"account-1"}, "symbol": {"EURUSD"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetDepthWithResponse(ctx, &GetDepthParams{Locale: "en", AccountId: "account-1", Symbol: "EURUSD"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "DepthResponse", file: "depth", check: checkResponseUnion(GetDepth200JSONResponseBody.AsDepthResponse, assertDepth)},
			}},
		{id: "getExecutions", request: responseRequest{method: "GET", path: "/accounts/account-1/executions", query: url.Values{"locale": {"en"}, "startTime": {"0"}, "endTime": {"1793153460"}, "maxCount": {"2"}, "cursor": {"input+/= cursor"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetExecutionsWithResponse(ctx, "account-1", &GetExecutionsParams{Locale: "en", StartTime: new(StartTime(0)), EndTime: new(EndTime(1793153460)), MaxCount: new(2.0), Cursor: new(Cursor("input+/= cursor"))})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "ExecutionsResponse", file: "executions", check: checkResponseUnion(GetExecutions200JSONResponseBody.AsExecutionsResponse, assertExecutions)},
				{schema: "ExecutionsResponse", file: "executions-empty", check: checkResponseUnion(GetExecutions200JSONResponseBody.AsExecutionsResponse, assertExecutionsEmpty)},
			}},
		{id: "getGroups", request: responseRequest{method: "GET", path: "/groups", query: url.Values{}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetGroupsWithResponse(ctx)
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "GroupListResponse", file: "groups", check: checkResponseUnion(GetGroups200JSONResponseBody.AsGroupListResponse, assertGroups)},
			}},
		{id: "getHistory", request: responseRequest{method: "GET", path: "/history", query: url.Values{"symbol": {"EURUSD"}, "resolution": {"1"}, "from": {"1793153400"}, "to": {"1793153460"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetHistoryWithResponse(ctx, &GetHistoryParams{Symbol: "EURUSD", Resolution: "1", From: 1793153400, To: 1793153460})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "HistorySuccessResponse", file: "history", check: checkResponseUnion(GetHistory200JSONResponseBody.AsHistorySuccessResponse, assertHistory)},
				{schema: "HistoryEmptyBarResponse", file: "history-empty", check: checkResponseUnion(GetHistory200JSONResponseBody.AsHistoryEmptyBarResponse, assertHistoryEmpty)},
			}},
		{id: "getIndividualPositions", request: responseRequest{method: "GET", path: "/accounts/account-1/individualPositions", query: url.Values{"locale": {"en"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetIndividualPositionsWithResponse(ctx, "account-1", &GetIndividualPositionsParams{Locale: "en"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "IndividualPositionsResponse", file: "individual-positions", check: checkResponseUnion(GetIndividualPositions200JSONResponseBody.AsIndividualPositionsResponse, assertIndividualPositions)},
			}},
		{id: "getInstruments", request: responseRequest{method: "GET", path: "/accounts/account-1/instruments", query: url.Values{"locale": {"en"}, "symbols": {"EURUSD,GBPUSD"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetInstrumentsWithResponse(ctx, "account-1", &GetInstrumentsParams{Locale: "en", Symbols: new([]string{"EURUSD", "GBPUSD"})})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "InstrumentsResponse", file: "instruments", check: checkResponseUnion(GetInstruments200JSONResponseBody.AsInstrumentsResponse, assertInstruments)},
			}},
		{id: "getLeverage", request: responseRequest{method: "POST", path: "/accounts/account-1/getLeverage", query: url.Values{"locale": {"en"}}, form: url.Values{"instrument": {"EURUSD"}, "orderType": {"limit"}, "side": {"buy"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetLeverageWithFormdataBodyWithResponse(ctx, "account-1", &GetLeverageParams{Locale: "en"}, GetLeverageFormdataRequestBody{Instrument: "EURUSD", OrderType: "limit", Side: "buy"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "GetLeverageResponse", file: "leverage", check: checkResponseUnion(GetLeverage200JSONResponseBody.AsGetLeveragePayload, assertLeverage)},
			}},
		{id: "getMapping", request: responseRequest{method: "GET", path: "/mapping", query: url.Values{}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetMappingWithResponse(ctx)
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "MappingResponse", file: "mapping", check: checkResponseDirect(assertMapping)},
			}},
		{id: "getNetPositions", request: responseRequest{method: "GET", path: "/accounts/account-1/netPositions", query: url.Values{"locale": {"en"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetNetPositionsWithResponse(ctx, "account-1", &GetNetPositionsParams{Locale: "en"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "NetPositionsResponse", file: "net-positions", check: checkResponseUnion(GetNetPositions200JSONResponseBody.AsNetPositionsResponse, assertNetPositions)},
			}},
		{id: "getOrders", request: responseRequest{method: "GET", path: "/accounts/account-1/orders", query: url.Values{"locale": {"en"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetOrdersWithResponse(ctx, "account-1", &GetOrdersParams{Locale: "en"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "OrdersResponse", file: "orders", check: checkResponseUnion(GetOrders200JSONResponseBody.AsOrdersResponse, assertOrders)},
			}},
		{id: "getOrdersHistory", request: responseRequest{method: "GET", path: "/accounts/account-1/ordersHistory", query: url.Values{"locale": {"en"}, "startTime": {"0"}, "endTime": {"1793153460"}, "maxCount": {"2"}, "cursor": {"input+/= cursor"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetOrdersHistoryWithResponse(ctx, "account-1", &GetOrdersHistoryParams{Locale: "en", StartTime: new(StartTime(0)), EndTime: new(EndTime(1793153460)), MaxCount: new(2.0), Cursor: new(Cursor("input+/= cursor"))})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "OrdersHistoryResponse", file: "orders-history", check: checkResponseUnion(GetOrdersHistory200JSONResponseBody.AsOrdersHistoryResponse, assertOrdersHistory)},
				{schema: "OrdersHistoryResponse", file: "orders-history-empty", check: checkResponseUnion(GetOrdersHistory200JSONResponseBody.AsOrdersHistoryResponse, assertOrdersHistoryEmpty)},
			}},
		{id: "getPermissions", request: responseRequest{method: "GET", path: "/permissions", query: url.Values{}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetPermissionsWithResponse(ctx)
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "PermissionsResponse", file: "permissions", check: checkResponseUnion(GetPermissions200JSONResponseBody.AsPermissionsResponse, assertPermissions)},
			}},
		{id: "getPositions", request: responseRequest{method: "GET", path: "/accounts/account-1/positions", query: url.Values{"locale": {"en"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetPositionsWithResponse(ctx, "account-1", &GetPositionsParams{Locale: "en"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "PositionsResponse", file: "positions", check: checkResponseUnion(GetPositions200JSONResponseBody.AsPositionsResponse, assertPositions)},
			}},
		{id: "getQuotes", request: responseRequest{method: "GET", path: "/quotes", query: url.Values{"locale": {"en"}, "accountId": {"account-1"}, "symbols": {"EURUSD,GBPUSD"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetQuotesWithResponse(ctx, &GetQuotesParams{Locale: "en", AccountId: "account-1", Symbols: "EURUSD,GBPUSD"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "QuotesResponse", file: "quotes", check: checkResponseUnion(GetQuotes200JSONResponseBody.AsQuotesResponse, assertQuotes)},
			}},
		{id: "getState", request: responseRequest{method: "GET", path: "/accounts/account-1/state", query: url.Values{"locale": {"en"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetStateWithResponse(ctx, "account-1", &GetStateParams{Locale: "en"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "AccountStateResponse", file: "state", check: checkResponseUnion(GetState200JSONResponseBody.AsAccountStateResponse, assertState)},
			}},
		{id: "getSymbolInfo", request: responseRequest{method: "GET", path: "/symbol_info", query: url.Values{"group": {"forex"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetSymbolInfoWithResponse(ctx, &GetSymbolInfoParams{Group: new(Group("forex"))})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "SymbolInfoResponse", file: "symbol-info-arrays", check: checkResponseUnion(GetSymbolInfo200JSONResponseBody.AsSymbolInfoResponse, assertSymbolInfo)},
				{schema: "SymbolInfoResponse", file: "symbol-info-scalars", check: checkResponseUnion(GetSymbolInfo200JSONResponseBody.AsSymbolInfoResponse, assertSymbolInfo)},
			}},
		{id: "getUserInfo", request: responseRequest{method: "GET", path: "/userInfo", query: url.Values{"locale": {"en"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.GetUserInfoWithResponse(ctx, &GetUserInfoParams{Locale: "en"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "UserInfoResponse", file: "user-info", check: checkResponseUnion(GetUserInfo200JSONResponseBody.AsUserInfoResponse, assertUserInfo)},
			}},
		{id: "logout", request: responseRequest{method: "POST", path: "/logout", query: url.Values{}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.LogoutWithResponse(ctx)
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "SuccessResponse", file: "success", check: checkResponseUnion(Logout200JSONResponseBody.AsSuccessResponse, assertSuccess)},
			}},
		{id: "modifyIndividualPosition", request: responseRequest{method: "PUT", path: "/accounts/account-1/individualPositions/position-1", query: url.Values{"locale": {"en"}}, form: url.Values{"stopLoss": {"0"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.ModifyIndividualPositionWithFormdataBodyWithResponse(ctx, "account-1", "position-1", &ModifyIndividualPositionParams{Locale: "en"}, ModifyIndividualPositionFormdataRequestBody{StopLoss: new(0.0)})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "SuccessResponse", file: "success", check: checkResponseUnion(ModifyIndividualPosition200JSONResponseBody.AsSuccessResponse, assertSuccess)},
				{schema: "SuccessResponseWithTransactionId", file: "transaction", check: checkResponseUnion(ModifyIndividualPosition200JSONResponseBody.AsSuccessResponseWithTransactionId, assertTransaction)},
			}},
		{id: "modifyOrder", request: responseRequest{method: "PUT", path: "/accounts/account-1/orders/order-1", query: url.Values{"locale": {"en"}}, form: url.Values{"instrument": {"EURUSD"}, "qty": {"2.5"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.ModifyOrderWithFormdataBodyWithResponse(ctx, "account-1", "order-1", &ModifyOrderParams{Locale: "en"}, ModifyOrderFormdataRequestBody{Instrument: "EURUSD", Qty: 2.5})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "SuccessResponse", file: "success", check: checkResponseUnion(ModifyOrder200JSONResponseBody.AsSuccessResponse, assertSuccess)},
				{schema: "SuccessResponseWithTransactionId", file: "transaction", check: checkResponseUnion(ModifyOrder200JSONResponseBody.AsSuccessResponseWithTransactionId, assertTransaction)},
			}},
		{id: "modifyPosition", request: responseRequest{method: "PUT", path: "/accounts/account-1/positions/position-1", query: url.Values{"locale": {"en"}}, form: url.Values{"stopLoss": {"0"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.ModifyPositionWithFormdataBodyWithResponse(ctx, "account-1", "position-1", &ModifyPositionParams{Locale: "en"}, ModifyPositionFormdataRequestBody{StopLoss: new(0.0)})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "SuccessResponse", file: "success", check: checkResponseUnion(ModifyPosition200JSONResponseBody.AsSuccessResponse, assertSuccess)},
				{schema: "SuccessResponseWithTransactionId", file: "transaction", check: checkResponseUnion(ModifyPosition200JSONResponseBody.AsSuccessResponseWithTransactionId, assertTransaction)},
			}},
		{id: "orderInfo", request: responseRequest{method: "POST", path: "/accounts/account-1/orderInfo", query: url.Values{"locale": {"en"}}, form: url.Values{"instrument": {"EURUSD"}, "qty": {"2.5"}, "side": {"buy"}, "type": {"limit"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.OrderInfoWithFormdataBodyWithResponse(ctx, "account-1", &OrderInfoParams{Locale: "en"}, OrderInfoFormdataRequestBody{Instrument: "EURUSD", Qty: 2.5, Side: "buy", Type: "limit"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "OrderInfoResponse", file: "order-info-text", check: checkResponseUnion(OrderInfo200JSONResponseBody.AsOrderInfoPayload, assertOrderInfoText)},
				{schema: "OrderInfoResponse", file: "order-info-markup", check: checkResponseUnion(OrderInfo200JSONResponseBody.AsOrderInfoPayload, assertOrderInfoMarkup)},
			}},
		{id: "placeOrder", request: responseRequest{method: "POST", path: "/accounts/account-1/orders", query: url.Values{"locale": {"en"}}, form: url.Values{"instrument": {"EURUSD"}, "qty": {"2.5"}, "side": {"buy"}, "type": {"limit"}, "currentAsk": {"1.25"}, "currentBid": {"1.24"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.PlaceOrderWithFormdataBodyWithResponse(ctx, "account-1", &PlaceOrderParams{Locale: "en"}, PlaceOrderFormdataRequestBody{Instrument: "EURUSD", Qty: 2.5, Side: "buy", Type: "limit", CurrentAsk: 1.25, CurrentBid: 1.24})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "PostOrderResponse", file: "place-order", check: checkResponseUnion(PlaceOrder200JSONResponseBody.AsPostOrderResponse, assertPlaceOrder)},
			}},
		{id: "postPromotionReport", request: responseRequest{method: "POST", path: "/promotionReport", query: url.Values{"locale": {"en"}}, jsonBody: `{"promotionId":"promotion-3"}`},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.PostPromotionReportWithResponse(ctx, &PostPromotionReportParams{Locale: "en"}, PostPromotionReportJSONRequestBody{PromotionId: "promotion-3"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "SuccessResponse", file: "success", check: checkResponseUnion(PostPromotionReport200JSONResponseBody.AsSuccessResponse, assertSuccess)},
			}},
		{id: "previewClosePosition", request: responseRequest{method: "POST", path: "/accounts/account-1/previewClosePosition", query: url.Values{"locale": {"en"}}, form: url.Values{"positionId": {"position-1"}, "qty": {"2.5"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.PreviewClosePositionWithFormdataBodyWithResponse(ctx, "account-1", &PreviewClosePositionParams{Locale: "en"}, PreviewClosePositionFormdataRequestBody{PositionId: "position-1", Qty: 2.5})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "PreviewClosePositionResponse", file: "preview-close-text", check: checkResponseUnion(PreviewClosePosition200JSONResponseBody.AsPreviewClosePositionPayload, assertPreviewCloseText)},
				{schema: "PreviewClosePositionResponse", file: "preview-close-markup", check: checkResponseUnion(PreviewClosePosition200JSONResponseBody.AsPreviewClosePositionPayload, assertPreviewCloseMarkup)},
			}},
		{id: "previewLeverage", request: responseRequest{method: "POST", path: "/accounts/account-1/previewLeverage", query: url.Values{"locale": {"en"}}, form: url.Values{"instrument": {"EURUSD"}, "orderType": {"limit"}, "side": {"buy"}, "leverage": {"2.5"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.PreviewLeverageWithFormdataBodyWithResponse(ctx, "account-1", &PreviewLeverageParams{Locale: "en"}, PreviewLeverageFormdataRequestBody{Instrument: "EURUSD", OrderType: "limit", Side: "buy", Leverage: 2.5})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "PreviewLeverageResponse", file: "preview-leverage", check: checkResponseUnion(PreviewLeverage200JSONResponseBody.AsPreviewLeveragePayload, assertPreviewLeverage)},
			}},
		{id: "previewOrder", request: responseRequest{method: "POST", path: "/accounts/account-1/previewOrder", query: url.Values{"locale": {"en"}}, form: url.Values{"instrument": {"EURUSD"}, "qty": {"2.5"}, "side": {"buy"}, "type": {"limit"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.PreviewOrderWithFormdataBodyWithResponse(ctx, "account-1", &PreviewOrderParams{Locale: "en"}, PreviewOrderFormdataRequestBody{Instrument: "EURUSD", Qty: 2.5, Side: "buy", Type: "limit"})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "PreviewOrderResponse", file: "preview-order", check: checkResponseUnion(PreviewOrder200JSONResponseBody.AsPreviewOrderPayload, assertPreviewOrder)},
			}},
		{id: "setLeverage", request: responseRequest{method: "POST", path: "/accounts/account-1/setLeverage", query: url.Values{"locale": {"en"}}, form: url.Values{"instrument": {"EURUSD"}, "orderType": {"limit"}, "side": {"buy"}, "leverage": {"2.5"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.SetLeverageWithFormdataBodyWithResponse(ctx, "account-1", &SetLeverageParams{Locale: "en"}, SetLeverageFormdataRequestBody{Instrument: "EURUSD", OrderType: "limit", Side: "buy", Leverage: 2.5})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "SetLeverageResponse", file: "set-leverage", check: checkResponseUnion(SetLeverage200JSONResponseBody.AsSetLeveragePayload, assertSetLeverage)},
			}},
		{id: "trackLatency", request: responseRequest{method: "POST", path: "/accounts/account-1/trackLatency", query: url.Values{}, form: url.Values{"instrument": {"EURUSD"}, "ask": {"1.25"}, "bid": {"1.24"}, "side": {"buy"}, "timestamp": {"1793153400"}, "transactionId": {"transaction-7"}, "transactionType": {"place_order"}, "roundTripDuration": {"12.5"}}},
			call: func(ctx context.Context, c *ClientWithResponses) (responseObservation, error) {
				r, err := c.TrackLatencyWithFormdataBodyWithResponse(ctx, "account-1", TrackLatencyFormdataRequestBody{Instrument: "EURUSD", Ask: 1.25, Bid: 1.24, Side: "buy", Timestamp: 1793153400, TransactionId: "transaction-7", TransactionType: "place_order", RoundTripDuration: 12.5})
				if err != nil {
					return responseObservation{}, err
				}
				return responseObservation{status: r.StatusCode(), body: r.Body, payload: r.JSON200}, nil
			},
			variants: []responseVariant{
				{schema: "SuccessResponse", file: "success", check: checkResponseDirect(assertSuccess)},
			}},
	}
}
