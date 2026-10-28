package tradeview

import "testing"

func responseDecode[T any](t *testing.T, decode func() (T, error)) T {
	t.Helper()
	value, err := decode()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertSuccess(t *testing.T, p SuccessResponse) { responseEqual(t, string(p.S), "ok") }

func assertTransaction(t *testing.T, p SuccessResponseWithTransactionId) {
	responseEqual(t, string(p.S), "ok")
	data := responsePresent(t, p.D)
	responseEqual(t, string(responsePresent(t, data.TransactionId)), "transaction-7")
}

func assertAuthorize(t *testing.T, p AuthorizePayload) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, p.D.AccessToken, "fixture-access-token")
	responseEqual(t, p.D.Expiration, 1793153400.0)
}

func assertAccounts(t *testing.T, p AccountResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, len(p.D), 2)
	a := p.D[0]
	responseEqual(t, a.Id, "account-1")
	responseEqual(t, a.Name, "Trading account")
	responseEqual(t, string(a.Type), "demo")
	responseEqual(t, responsePresent(t, a.Currency), "USD")
	responseEqual(t, responsePresent(t, a.IsVerified), false)
	responseEqual(t, responsePresent(t, a.Config.SupportPositions), true)
	responseEqual(t, responsePresent(t, a.Config.SupportLeverage), false)
	responseEqual(t, a.Config.SupportBrackets == nil, true)
	sets := responsePresent(t, a.TradingOptionsSets)
	forex, ok := sets["forex"]
	responseEqual(t, ok, true)
	responseEqual(t, responsePresent(t, forex.AllowedOrderTypes), []TradingOptionsSetsAllowedOrderTypes{"market", "limit"})
	responseEqual(t, responsePresent(t, forex.SupportCryptoExchange), false)
	ui := responsePresent(t, a.Ui)
	links := responsePresent(t, ui.OrderDialogLinks)
	responseEqual(t, links, []Hyperlink{{Text: "Terms", Url: "https://broker.example/terms"}})
	responseEqual(t, p.D[1].Id, "account-2")
	responseEqual(t, string(p.D[1].Type), "live")
	responseEqual(t, p.D[1].Currency == nil && p.D[1].IsVerified == nil && p.D[1].Ui == nil, true)
}

func assertBalances(t *testing.T, p CryptoBalancesResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, len(p.D), 2)
	responseEqual(t, p.D[0].Symbol, "BTC")
	responseEqual(t, p.D[0].Available, 0.123456789012345)
	responseEqual(t, p.D[0].Total, 0.25)
	responseEqual(t, responsePresent(t, p.D[0].Reserved), 0.0)
	responseEqual(t, responsePresent(t, p.D[0].ValueCurrency), "USD")
	responseEqual(t, p.D[1].Symbol, "USD")
	responseEqual(t, p.D[1].Available, 0.0)
	responseEqual(t, p.D[1].Reserved == nil && p.D[1].ValueCurrency == nil, true)
}

func assertConfiguration(t *testing.T, p ConfigResponse) {
	responseEqual(t, string(p.S), "ok")
	interval := responsePresent(t, p.D.PullingInterval)
	responseEqual(t, responsePresent(t, interval.Quotes), 250.0)
	responseEqual(t, responsePresent(t, interval.Orders), 500.0)
	responseEqual(t, interval.Positions == nil, true)
	info := responsePresent(t, p.D.OrderInfoConfig)
	responseEqual(t, info.Dependencies, []OrderInfoConfigDependencies{"qty", "side"})
	responseEqual(t, responsePresent(t, info.DependantCustomFieldIds), []string{"routing"})
}

func assertDepth(t *testing.T, p DepthResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, p.D.Asks, []DepthItem{{1.23456789012345, 12.5}, {1.25, 20}})
	responseEqual(t, p.D.Bids, []DepthItem{{1.22, 5}})
}

func assertExecutions(t *testing.T, p ExecutionsResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, len(p.D), 1)
	e := p.D[0]
	responseEqual(t, e.Id, "execution-1")
	responseEqual(t, e.Instrument, "EURUSD")
	responseEqual(t, e.OrderId, "order-1")
	responseEqual(t, responsePresent(t, e.PositionId), "position-1")
	responseEqual(t, e.Price, 1.23456789012345)
	responseEqual(t, e.Qty, 2.5)
	responseEqual(t, string(e.Side), "buy")
	responseEqual(t, e.IsClose, false)
	responseEqual(t, e.Time, 1793153400.0)
	responseEqual(t, responsePresent(t, e.Commission), 0.0)
	responseEqual(t, responsePresent(t, e.Fee), 0.125)
	responseEqual(t, responsePresent(t, e.GrossPnl), 10.5)
	responseEqual(t, responsePresent(t, e.NetPnl), 10.375)
	responseEqual(t, responsePresent(t, e.Swap), -0.25)
	responseEqual(t, responsePresent(t, e.Conversion), 1.1)
	responseEqual(t, responsePresent(t, p.NextPageCursor), "next+/= cursor")
}

func assertExecutionsEmpty(t *testing.T, p ExecutionsResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, p.D, []Execution{})
	responseEqual(t, p.NextPageCursor == nil, true)
}

func assertGroups(t *testing.T, p GroupListResponse) {
	responseEqual(t, string(p.S), "ok")
	groups := responsePresent(t, p.D.Groups)
	responseEqual(t, len(groups), 2)
	responseEqual(t, groups[0].Id, "forex")
	responseEqual(t, groups[1].Id, "stocks")
}

func assertHistory(t *testing.T, p HistorySuccessResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, p.T, []float64{1793153400, 1793153460})
	responseEqual(t, p.O, []float64{1.2, 1.3})
	responseEqual(t, p.H, []float64{1.4, 1.5})
	responseEqual(t, p.L, []float64{1.1, 1.2})
	responseEqual(t, p.C, []float64{1.3, 1.4})
	responseEqual(t, p.V, []float64{12.5, 20.25})
}

func assertHistoryEmpty(t *testing.T, p HistoryEmptyBarResponse) {
	responseEqual(t, string(p.S), "ok")
	for _, field := range []*[]float64{p.T, p.O, p.H, p.L, p.C, p.V} {
		responseEqual(t, responsePresent(t, field), []float64{})
	}
}

func assertIndividualPositions(t *testing.T, p IndividualPositionsResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, len(p.D), 1)
	position := p.D[0]
	responseEqual(t, position.Id, "position-1")
	responseEqual(t, position.Instrument, "EURUSD")
	responseEqual(t, position.Date, "2026-10-28T12:00:00Z")
	responseEqual(t, position.Qty, 2.5)
	responseEqual(t, string(position.Side), "buy")
	responseEqual(t, position.Price, 1.23456789012345)
	responseEqual(t, responsePresent(t, position.CanBeClosed), false)
	responseEqual(t, responsePresent(t, position.UnrealizedPl), 0.0)
	responseEqual(t, responsePresent(t, position.LastModified), 1793153400.0)
	responseEqual(t, position.Message == nil, true)
}

func assertInstruments(t *testing.T, p InstrumentsResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, len(p.D), 1)
	i := p.D[0]
	responseEqual(t, i.Name, "EURUSD")
	responseEqual(t, i.Description, "Euro / US dollar")
	responseEqual(t, i.MinTick, 0.00001)
	responseEqual(t, i.PipSize, 0.0001)
	responseEqual(t, i.PipValue, 1.25)
	responseEqual(t, string(i.Type), "forex")
	responseEqual(t, responsePresent(t, i.HasQuotes), false)
	responseEqual(t, responsePresent(t, i.MinQty), 0.0)
	responseEqual(t, i.MaxQty == nil, true)
	responseEqual(t, responsePresent(t, i.TradingOptionsSet), "forex")
	ui := responsePresent(t, i.Ui)
	rules := responsePresent(t, ui.ValidationRules)
	responseEqual(t, len(rules), 5)
	stop := responseDecode(t, rules[0].AsStopPercentValidationRule)
	responseEqual(t, string(stop.Id), "stopPercent")
	responseEqual(t, stop.Options.Min, 0.125)
	responseEqual(t, stop.Options.Max, 1.5)
	limit := responseDecode(t, rules[1].AsLimitPercentValidationRule)
	responseEqual(t, string(limit.Id), "limitPercent")
	responseEqual(t, limit.Options.Min, 1.125)
	responseEqual(t, limit.Options.Max, 2.5)
	sl := responseDecode(t, rules[2].AsStopLossPercentValidationRule)
	responseEqual(t, string(sl.Id), "slPercent")
	responseEqual(t, sl.Options.Min, 2.125)
	responseEqual(t, sl.Options.Max, 3.5)
	tp := responseDecode(t, rules[3].AsTakeProfitPercentValidationRule)
	responseEqual(t, string(tp.Id), "tpPercent")
	responseEqual(t, tp.Options.Min, 3.125)
	responseEqual(t, tp.Options.Max, 4.5)
	gs := responseDecode(t, rules[4].AsGuaranteedStopPercentValidationRule)
	responseEqual(t, string(gs.Id), "gsPercent")
	responseEqual(t, gs.Options.Min, 4.125)
	responseEqual(t, gs.Options.Max, 5.5)
}

func assertLeverage(t *testing.T, p GetLeveragePayload) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, p.D, GetLeverage{Title: "Leverage", Leverage: 2.5, Min: 1, Max: 30, Step: 0.5})
}

func assertMapping(t *testing.T, p MappingResponse) {
	responseEqual(t, responsePresent(t, p.Fields), []SingleField{"brokerSymbol"})
	responseEqual(t, responsePresent(t, p.Symbols), []SingleMapping{{F: []string{"EURUSD"}, S: "FX_IDC:EURUSD"}})
}

func assertNetPositions(t *testing.T, p NetPositionsResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, len(p.D), 1)
	position := p.D[0]
	responseEqual(t, position.Id, "position-1")
	responseEqual(t, position.Instrument, "EURUSD")
	responseEqual(t, position.Qty, 2.5)
	responseEqual(t, responsePresent(t, position.AvgPrice), 1.23456789012345)
	responseEqual(t, responsePresent(t, position.LongQty), 2.5)
	responseEqual(t, responsePresent(t, position.ShortQty), 0.0)
	responseEqual(t, position.Message == nil, true)
}

func assertOrders(t *testing.T, p OrdersResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, len(p.D), 2)
	o := p.D[0]
	responseEqual(t, o.Id, "order-1")
	responseEqual(t, o.Instrument, "EURUSD")
	responseEqual(t, o.Qty, 2.5)
	responseEqual(t, string(o.Side), "buy")
	responseEqual(t, string(o.Type), "limit")
	responseEqual(t, string(o.Status), "working")
	responseEqual(t, responsePresent(t, o.LimitPrice), 1.23456789012345)
	responseEqual(t, responsePresent(t, o.FilledQty), 0.0)
	responseEqual(t, responsePresent(t, o.IsTrailingStop), false)
	duration := responsePresent(t, o.Duration)
	responseEqual(t, duration.Type, "gtt")
	responseEqual(t, responsePresent(t, duration.Datetime), 1793153400.0)
	responseEqual(t, responsePresent(t, o.CustomFields), []CustomFieldsValueItem{{Id: "commission", Value: "1.25"}})
	responseEqual(t, p.D[1].Id, "order-2")
	responseEqual(t, string(p.D[1].Side), "sell")
	responseEqual(t, p.D[1].Duration == nil && p.D[1].FilledQty == nil && p.D[1].IsTrailingStop == nil, true)
}

func assertOrdersHistory(t *testing.T, p OrdersHistoryResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, len(p.D), 1)
	o := p.D[0]
	responseEqual(t, o.Id, "order-1")
	responseEqual(t, o.Instrument, "EURUSD")
	responseEqual(t, o.Qty, 2.5)
	responseEqual(t, string(o.Side), "buy")
	responseEqual(t, string(o.Type), "limit")
	responseEqual(t, string(o.Status), "filled")
	responseEqual(t, responsePresent(t, o.LimitPrice), 1.23456789012345)
	responseEqual(t, responsePresent(t, o.FilledQty), 0.0)
	responseEqual(t, responsePresent(t, o.IsTrailingStop), false)
	duration := responsePresent(t, o.Duration)
	responseEqual(t, duration.Type, "gtt")
	responseEqual(t, responsePresent(t, duration.Datetime), 1793153400.0)
	responseEqual(t, responsePresent(t, o.CustomFields), []CustomFieldsValueItem{{Id: "commission", Value: "1.25"}})
	responseEqual(t, responsePresent(t, p.NextPageCursor), "orders+/= cursor")
}

func assertOrdersHistoryEmpty(t *testing.T, p OrdersHistoryResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, p.D, []OrderHistory{})
	responseEqual(t, p.NextPageCursor == nil, true)
}

func assertPermissions(t *testing.T, p PermissionsResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, p.D.Groups, []string{"forex", "stocks"})
}

func assertPositions(t *testing.T, p PositionsResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, len(p.D), 1)
	position := p.D[0]
	responseEqual(t, position.Id, "position-1")
	responseEqual(t, position.Instrument, "EURUSD")
	responseEqual(t, position.Qty, 2.5)
	responseEqual(t, string(position.Side), "buy")
	responseEqual(t, position.AvgPrice, 1.23456789012345)
	responseEqual(t, responsePresent(t, position.UnrealizedPl), 0.0)
	responseEqual(t, responsePresent(t, position.CustomFields), []CustomFieldsValueItem{{Id: "risk", Value: "low"}})
	responseEqual(t, position.LastModified == nil, true)
}

func assertQuotes(t *testing.T, p QuotesResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, len(p.D), 2)
	responseEqual(t, p.D[0].N, "EURUSD")
	responseEqual(t, string(responsePresent(t, p.D[0].S)), "ok")
	responseEqual(t, p.D[0].V.Ask, 1.23456789012345)
	responseEqual(t, p.D[0].V.Bid, 1.22)
	responseEqual(t, responsePresent(t, p.D[0].V.AskSize), 0.0)
	responseEqual(t, responsePresent(t, p.D[0].V.Halted), false)
	responseEqual(t, p.D[1].N, "GBPUSD")
	responseEqual(t, p.D[1].S == nil && p.D[1].V.AskSize == nil && p.D[1].V.Halted == nil, true)
}

func assertState(t *testing.T, p AccountStateResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, p.D.Balance, 1234.56789012345)
	responseEqual(t, p.D.UnrealizedPl, -2.25)
	responseEqual(t, responsePresent(t, p.D.Equity), 0.0)
	responseEqual(t, p.D.MarginAvailable == nil, true)
	responseEqual(t, responsePresent(t, p.D.AccountSummaryRowData), []string{"USD", "active"})
	responseEqual(t, responsePresent(t, p.D.AmData), [][][]string{{{"EURUSD", "2.5"}}})
}

func assertSymbolInfo(t *testing.T, p SymbolInfoResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, p.Symbol, []string{"EURUSD", "GBPUSD"})
	responseEqual(t, p.Description, []string{"Euro / US dollar", "Pound / US dollar"})
	responseEqual(t, p.ExchangeListed, []string{"BROKER", "BROKER"})
	responseEqual(t, p.ExchangeTraded, []string{"BROKER", "BROKER"})
	responseEqual(t, p.Pricescale, []float64{100000, 100000})
	responseEqual(t, p.Minmovement, []float64{1, 1})
	responseEqual(t, p.SessionRegular, []string{"24x7", "24x7"})
	responseEqual(t, p.Timezone, []SymbolInfoResponseTimezone{"Etc/UTC", "Etc/UTC"})
	responseEqual(t, p.Type, []SymbolType{"forex", "forex"})
	responseEqual(t, responsePresent(t, p.HasDaily), []bool{false, false})
	responseEqual(t, p.HasIntraday == nil, true)
	currency := responsePresent(t, p.Currency)
	responseEqual(t, len(currency), 2)
	for _, c := range currency {
		responseEqual(t, responsePresent(t, c), "USD")
	}
}

func assertUserInfo(t *testing.T, p UserInfoResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, p.D.UserId, "user-9")
	responseEqual(t, responsePresent(t, p.D.PromotionId), "promotion-3")
}

func assertPlaceOrder(t *testing.T, p PostOrderResponse) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, responsePresent(t, p.D.OrderId), "order-1")
	responseEqual(t, string(responsePresent(t, p.D.TransactionId)), "transaction-7")
}

func assertMarkup(t *testing.T, markup MarkupText) {
	t.Helper()
	responseEqual(t, len(markup), 3)
	responseEqual(t, responseDecode(t, markup[0].AsMarkupText0), "Read ")
	link := responseDecode(t, markup[1].AsHyperlink)
	responseEqual(t, link, Hyperlink{Text: "terms", Url: "https://broker.example/terms"})
	responseEqual(t, responseDecode(t, markup[2].AsMarkupText0), ".")
}

func assertOrderInfo(t *testing.T, p OrderInfoPayload) {
	t.Helper()
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, p.D.Id, "info-1")
	responseEqual(t, len(p.D.Rows), 3)
	row := responseDecode(t, p.D.Rows[0].AsOrderInfoRow)
	responseEqual(t, row.Title, "Fee")
	responseEqual(t, responsePresent(t, row.Value), "1.25")
	responseEqual(t, responsePresent(t, row.Currency), "USD")
	divider := responseDecode(t, p.D.Rows[1].AsOrderInfoDivider)
	responseEqual(t, string(divider.Type), "separator")
	subheading := responseDecode(t, p.D.Rows[2].AsOrderInfoSubheading)
	responseEqual(t, string(subheading.Type), "subheading")
	responseEqual(t, subheading.Value, "Details")
}

func assertOrderInfoText(t *testing.T, p OrderInfoPayload) {
	assertOrderInfo(t, p)
	disclaimer := responsePresent(t, p.D.Disclaimer)
	responseEqual(t, responseDecode(t, disclaimer.AsOrderInfoDisclaimer0), "Plain disclaimer")
}

func assertOrderInfoMarkup(t *testing.T, p OrderInfoPayload) {
	assertOrderInfo(t, p)
	disclaimer := responsePresent(t, p.D.Disclaimer)
	assertMarkup(t, responseDecode(t, disclaimer.AsMarkupText))
}

func assertPreviewClose(t *testing.T, p PreviewClosePositionPayload) {
	t.Helper()
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, responsePresent(t, p.D.Id), "close-1")
	responseEqual(t, len(p.D.Rows), 2)
	row := responseDecode(t, p.D.Rows[0].AsPreviewClosePositionRow)
	responseEqual(t, row.Title, "Closing cost")
	responseEqual(t, responsePresent(t, row.Value), "2.50")
	responseEqual(t, responsePresent(t, row.Currency), "USD")
	divider := responseDecode(t, p.D.Rows[1].AsPreviewClosePositionDivider)
	responseEqual(t, string(divider.Type), "separator")
}

func assertPreviewCloseText(t *testing.T, p PreviewClosePositionPayload) {
	assertPreviewClose(t, p)
	disclaimer := responsePresent(t, p.D.Disclaimer)
	responseEqual(t, responseDecode(t, disclaimer.AsPreviewClosePositionDisclaimer0), "Plain disclaimer")
}

func assertPreviewCloseMarkup(t *testing.T, p PreviewClosePositionPayload) {
	assertPreviewClose(t, p)
	disclaimer := responsePresent(t, p.D.Disclaimer)
	assertMarkup(t, responseDecode(t, disclaimer.AsMarkupText))
}

func assertPreviewLeverage(t *testing.T, p PreviewLeveragePayload) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, responsePresent(t, p.D.Infos), []string{"New margin: 2.50"})
	responseEqual(t, responsePresent(t, p.D.Warnings), []string{"Leverage increases risk"})
	responseEqual(t, p.D.Errors == nil, true)
}

func assertPreviewOrder(t *testing.T, p PreviewOrderPayload) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, responsePresent(t, p.D.ConfirmId), "confirm-1")
	responseEqual(t, len(p.D.Sections), 2)
	responseEqual(t, responsePresent(t, p.D.Sections[0].Header), "Estimate")
	responseEqual(t, p.D.Sections[0].Rows, []OrderPreviewSectionRow{{Title: "Commission", Value: "0.125"}})
	responseEqual(t, p.D.Sections[1].Header == nil, true)
	responseEqual(t, p.D.Sections[1].Rows, []OrderPreviewSectionRow{})
	warnings := responsePresent(t, p.D.Warnings)
	responseEqual(t, len(warnings), 2)
	responseEqual(t, responseDecode(t, warnings[0].AsPreviewOrderWarnings0), "Warning")
	assertMarkup(t, responseDecode(t, warnings[1].AsMarkupText))
	errors := responsePresent(t, p.D.Errors)
	responseEqual(t, len(errors), 2)
	responseEqual(t, responseDecode(t, errors[0].AsPreviewOrderErrors0), "Error")
	assertMarkup(t, responseDecode(t, errors[1].AsMarkupText))
}

func assertSetLeverage(t *testing.T, p SetLeveragePayload) {
	responseEqual(t, string(p.S), "ok")
	responseEqual(t, p.D.Leverage, 2.5)
}
