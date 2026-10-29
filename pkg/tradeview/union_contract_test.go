package tradeview

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"reflect"
	"strings"
	"testing"
)

type jsonUnion interface {
	json.Marshaler
	json.Unmarshaler
}

// Keep the public union inventory explicit. The AST check below rejects new
// unions without tests; it inspects declarations, never implementation bodies.
var _unionContracts = []jsonUnion{
	new(Instrument_Ui_ValidationRules_Item), new(MarkupText_Item),
	new(OrderInfo_Disclaimer), new(OrderInfo_Rows_Item),
	new(PlaceModifyPreviewOrderCommon_ExitLevels_Item),
	new(PreviewClosePosition_Disclaimer), new(PreviewClosePosition_Rows_Item),
	new(PreviewOrder_Errors_Item), new(PreviewOrder_Warnings_Item),
	new(StreamIndividualPositionsMessage_Item), new(StreamNetPositionsMessage_Item), new(StreamPositionsMessage_Item),
	new(GetAccounts200JSONResponseBody), new(GetBalances200JSONResponseBody),
	new(GetExecutions200JSONResponseBody), new(GetLeverage200JSONResponseBody),
	new(GetIndividualPositions200JSONResponseBody), new(CloseIndividualPosition200JSONResponseBody),
	new(ModifyIndividualPosition200JSONResponseBody), new(GetInstruments200JSONResponseBody),
	new(GetNetPositions200JSONResponseBody), new(CloseNetPosition200JSONResponseBody),
	new(OrderInfo200JSONResponseBody), new(GetOrders200JSONResponseBody),
	new(PlaceOrderFormdataBody_ExitLevels_Item), new(PlaceOrder200JSONResponseBody),
	new(CancelAllOrdersFormdataBody), new(CancelAllOrders200JSONResponseBody), new(CancelOrder200JSONResponseBody),
	new(ModifyOrderFormdataBody_ExitLevels_Item), new(ModifyOrder200JSONResponseBody),
	new(GetOrdersHistory200JSONResponseBody), new(GetPositions200JSONResponseBody),
	new(ClosePosition200JSONResponseBody), new(ModifyPosition200JSONResponseBody),
	new(PreviewClosePosition200JSONResponseBody), new(PreviewLeverage200JSONResponseBody),
	new(PreviewOrderFormdataBody_ExitLevels_Item), new(PreviewOrder200JSONResponseBody),
	new(SetLeverage200JSONResponseBody), new(GetState200JSONResponseBody),
	new(StreamIndividualPositions200JSONResponseBody), new(StreamNetPositions200JSONResponseBody),
	new(StreamNotifications200JSONResponseBody), new(StreamOrders200JSONResponseBody),
	new(StreamPositions200JSONResponseBody), new(StreamState200JSONResponseBody),
	new(Authorize200JSONResponseBody), new(GetConfiguration200JSONResponseBody),
	new(GetDepth200JSONResponseBody), new(GetGroups200JSONResponseBody), new(GetHistory200JSONResponseBody),
	new(Logout200JSONResponseBody), new(GetPermissions200JSONResponseBody), new(PostPromotionReport200JSONResponseBody),
	new(GetQuotes200JSONResponseBody), new(StreamDepth200JSONResponseBody), new(StreamQuotes200JSONResponseBody),
	new(Streaming200JSONResponseBody), new(GetSymbolInfo200JSONResponseBody), new(GetUserInfo200JSONResponseBody),
}

func unionAlternativeJSON(t *testing.T, name string) []byte {
	t.Helper()
	files := map[string]string{
		"AccountResponse": "accounts", "CryptoBalancesResponse": "balances",
		"ExecutionsResponse": "executions", "GetLeveragePayload": "leverage",
		"IndividualPositionsResponse": "individual-positions", "InstrumentsResponse": "instruments",
		"NetPositionsResponse": "net-positions", "OrderInfoPayload": "order-info-markup",
		"OrdersResponse": "orders", "PostOrderResponse": "place-order", "OrdersHistoryResponse": "orders-history",
		"PositionsResponse": "positions", "PreviewClosePositionPayload": "preview-close-markup",
		"PreviewLeveragePayload": "preview-leverage", "PreviewOrderPayload": "preview-order",
		"SetLeveragePayload": "set-leverage", "AccountStateResponse": "state",
		"AuthorizePayload": "authorize", "ConfigResponse": "configuration", "DepthResponse": "depth",
		"GroupListResponse": "groups", "HistorySuccessResponse": "history", "HistoryEmptyBarResponse": "history-empty",
		"PermissionsResponse": "permissions", "QuotesResponse": "quotes", "SymbolInfoResponse": "symbol-info-arrays",
		"UserInfoResponse": "user-info", "SuccessResponse": "success",
		"SuccessResponseWithTransactionId": "transaction", "ErrorResponse": "broker-error",
	}
	if file, ok := files[name]; ok {
		return readResponseFixture(t, file)
	}
	// Independent wire examples also cover aliases (strings and arrays), nested
	// union models, and the generated raw streaming response unions.
	examples := map[string]string{
		"StopPercentValidationRule":           `{"id":"stopPercent","options":{"min":0.125,"max":1.5}}`,
		"LimitPercentValidationRule":          `{"id":"limitPercent","options":{"min":1.125,"max":2.5}}`,
		"StopLossPercentValidationRule":       `{"id":"slPercent","options":{"min":2.125,"max":3.5}}`,
		"TakeProfitPercentValidationRule":     `{"id":"tpPercent","options":{"min":3.125,"max":4.5}}`,
		"GuaranteedStopPercentValidationRule": `{"id":"gsPercent","options":{"min":4.125,"max":5.5}}`,
		"MarkupText0":                         `"Read terms"`, "Hyperlink": `{"text":"terms","url":"https://broker.example/terms"}`,
		"MarkupText":           `["Read ",{"text":"terms","url":"https://broker.example/terms"}]`,
		"OrderInfoDisclaimer0": `"Order disclaimer"`, "PreviewClosePositionDisclaimer0": `"Close disclaimer"`,
		"PreviewOrderErrors0": `"Order error"`, "PreviewOrderWarnings0": `"Order warning"`,
		"OrderInfoRow":     `{"title":"Commission","value":"1.25","currency":"USD"}`,
		"OrderInfoDivider": `{"type":"separator"}`, "OrderInfoSubheading": `{"type":"subheading","value":"Costs"}`,
		"PreviewClosePositionRow":            `{"title":"Close cost","value":"2.5","currency":"USD"}`,
		"PreviewClosePositionDivider":        `{"type":"separator"}`,
		"ExitLevel":                          `{"qty":1.25,"stopLoss":0,"takeProfit":2.5}`,
		"PlacedExitLevel":                    `{"id":"exit-1","qty":2.5,"stopLoss":1.25}`,
		"CancelAllOrdersByIdsRequestBody":    `{"orderIds":["order 1","order/2"]}`,
		"CancelAllOrdersBySymbolRequestBody": `{"symbol":"EURUSD","side":"buy"}`,
		"IndividualPosition":                 `{"id":"ip1","instrument":"EURUSD","qty":2.5,"side":"buy","price":1.25,"pl":3.5}`,
		"NetPosition":                        `{"id":"np1","instrument":"EURUSD","qty":2.5,"side":"buy","avgPrice":1.25,"longQty":2.5,"shortQty":0}`,
		"Position":                           `{"id":"p1","instrument":"EURUSD","qty":2.5,"side":"buy","avgPrice":1.25,"unrealizedPl":3.5}`,
		"StreamClosedPosition":               `{"id":"closed-1","qty":0}`,
		"StreamIndividualPositionsMessage":   `[{"id":"ip1","qty":0}]`,
		"StreamNetPositionsMessage":          `[{"id":"np1","qty":0}]`, "StreamPositionsMessage": `[{"id":"p1","qty":0}]`,
		"StreamNotificationsMessage": `{"key":"margin","severity":"error","bodyValues":["USD"]}`,
		"OrdersMessage":              `[{"id":"order-1","instrument":"EURUSD","qty":2.5,"side":"buy","type":"limit","status":"working"}]`,
		"AccountStateMessage":        `{"balance":12.5,"unrealizedPl":0}`,
		"Depth":                      `{"asks":[[1.25,2.5]],"bids":[[1.125,3.5]]}`,
		"QuotesMessage":              `[{"n":"EURUSD","v":{"bid":1.125,"ask":1.25}}]`,
		"PingMessage":                `{"s":"ok"}`, "ErrorMessage": `{"s":"error","errmsg":"denied"}`,
		"StreamingQuoteResponse":         `{"f":"q","id":"EURUSD","ap":1.25,"as":2.5,"bp":1.125,"bs":3.5,"t":1793153400}`,
		"StreamingTradeResponse":         `{"f":"t","id":"EURUSD","p":1.25,"s":2.5,"t":1793153400}`,
		"StreamingDailyBarResponse":      `{"f":"d","id":"EURUSD","o":1.25,"h":1.5,"l":1.125,"c":1.375,"v":12.5,"t":1793153400}`,
		"StreamingHeartbeatResponse":     `{"heartbeat":1793153400}`,
		"StreamingAskResponseDeprecated": `{"f":"a","id":"EURUSD","p":1.25,"s":2.5,"t":1793153400}`,
		"StreamingBidResponseDeprecated": `{"f":"b","id":"EURUSD","p":1.125,"s":3.5,"t":1793153400}`,
	}
	if example, ok := examples[name]; ok {
		return []byte(example)
	}
	t.Fatalf("missing independently authored union alternative %s", name)
	return nil
}

func unionJSONValue(t *testing.T, raw []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func assertUnionJSON(t *testing.T, value any, want any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if got := unionJSONValue(t, raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON = %s, want %#v", raw, want)
	}
}

func callUnionEditor(t *testing.T, method reflect.Value, value reflect.Value) error {
	t.Helper()
	result := method.Call([]reflect.Value{value})
	if result[0].IsNil() {
		return nil
	}
	err, ok := result[0].Interface().(error)
	if !ok {
		t.Fatal("union editor did not return an error")
	}
	return err
}

func TestPublicUnionContracts(t *testing.T) {
	tested := make(map[string]bool)
	for _, prototype := range _unionContracts {
		unionType := reflect.TypeOf(prototype)
		for method := range unionType.Methods() {
			if !strings.HasPrefix(method.Name, "From") {
				continue
			}
			alternative := strings.TrimPrefix(method.Name, "From")
			tested[unionType.Elem().Name()+".Merge"+alternative] = true
			t.Run(unionType.Elem().Name()+"/"+alternative, func(t *testing.T) {
				value := reflect.New(method.Type.In(1))
				if err := json.Unmarshal(unionAlternativeJSON(t, alternative), value.Interface()); err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(value.Interface())
				if err != nil {
					t.Fatal(err)
				}
				want := unionJSONValue(t, encoded)
				unionValue := reflect.New(unionType.Elem())
				union, ok := unionValue.Interface().(jsonUnion)
				if !ok {
					t.Fatal("union lost its JSON interfaces")
				}
				if err := union.UnmarshalJSON([]byte(`{"old":"must be replaced"}`)); err != nil {
					t.Fatal(err)
				}
				if err := callUnionEditor(t, unionValue.MethodByName(method.Name), value.Elem()); err != nil {
					t.Fatal(err)
				}
				assertUnionJSON(t, union, want)
				decoded := unionValue.MethodByName("As" + alternative).Call(nil)
				if !decoded[1].IsNil() {
					t.Fatal(decoded[1].Interface())
				}
				assertUnionJSON(t, decoded[0].Interface(), want)
				// Exercise Merge for every alternative. Object patches must also
				// preserve unrelated extensions and integers beyond float64 precision.
				if object, ok := want.(map[string]any); ok {
					if err := union.UnmarshalJSON([]byte(`{"extension":{"keep":true},"large":9007199254740993}`)); err != nil {
						t.Fatal(err)
					}
					object["extension"] = map[string]any{"keep": true}
					object["large"] = json.Number("9007199254740993")
				}
				if err := callUnionEditor(t, unionValue.MethodByName("Merge"+alternative), value.Elem()); err != nil {
					t.Fatal(err)
				}
				assertUnionJSON(t, union, want)
				if _, object := want.(map[string]any); object {
					// A fresh object union must accept its first patch as well.
					unionValue = reflect.New(unionType.Elem())
					if err := callUnionEditor(t, unionValue.MethodByName("Merge"+alternative), value.Elem()); err != nil {
						t.Fatal(err)
					}
					assertUnionJSON(t, unionValue.Interface(), unionJSONValue(t, encoded))
				}
			})
		}
	}
	file, err := parser.ParseFile(token.NewFileSet(), "api.gen.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		method, ok := declaration.(*ast.FuncDecl)
		if !ok || method.Recv == nil || !strings.HasPrefix(method.Name.Name, "Merge") {
			continue
		}
		receiver, ok := method.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			t.Fatal("unexpected union receiver")
		}
		name, ok := receiver.X.(*ast.Ident)
		if !ok || !tested[name.Name+"."+method.Name.Name] {
			t.Fatalf("public union method lacks a contract test: %v.%s", receiver.X, method.Name.Name)
		}
	}
}

func TestUnionNestedMergeAndErrors(t *testing.T) {
	var union Instrument_Ui_ValidationRules_Item
	if err := union.UnmarshalJSON([]byte(`{"id":"old","options":{"min":9,"max":10,"keep":false,"large":9007199254740993},"root":"retain"}`)); err != nil {
		t.Fatal(err)
	}
	var patch StopPercentValidationRule
	if err := json.Unmarshal([]byte(`{"id":"stopPercent","options":{"min":0,"max":2.5}}`), &patch); err != nil {
		t.Fatal(err)
	}
	if err := union.MergeStopPercentValidationRule(patch); err != nil {
		t.Fatal(err)
	}
	assertUnionJSON(t, union, unionJSONValue(t, []byte(`{"id":"stopPercent","options":{"min":0,"max":2.5,"keep":false,"large":9007199254740993},"root":"retain"}`)))
	decoded, err := union.AsStopPercentValidationRule()
	if err != nil || decoded.Id != "stopPercent" || decoded.Options.Min != 0 || decoded.Options.Max != 2.5 {
		t.Fatalf("nested merge lost typed values: %+v, %v", decoded, err)
	}
	var exits PlaceOrderFormdataBody_ExitLevels_Item
	if err := exits.FromExitLevel(ExitLevel{Qty: 1.25, StopLoss: new(0.0)}); err != nil {
		t.Fatal(err)
	}
	if err := exits.MergePlacedExitLevel(PlacedExitLevel{Id: "exit-1", Qty: 2.5, TakeProfit: new(3.5)}); err != nil {
		t.Fatal(err)
	}
	assertUnionJSON(t, exits, unionJSONValue(t, []byte(`{"id":"exit-1","qty":2.5,"stopLoss":0,"takeProfit":3.5}`)))
	for _, edit := range []func(ExitLevel) error{exits.FromExitLevel, exits.MergeExitLevel} {
		if err := edit(ExitLevel{Qty: math.NaN()}); err == nil {
			t.Fatal("non-JSON patch accepted")
		}
	}
	// RawMessage's public decoder accepts arbitrary bytes; merging malformed
	// stored JSON must report the dependency's decode error, never success.
	if err := exits.UnmarshalJSON([]byte("{")); err != nil {
		t.Fatal(err)
	}
	if _, err := exits.AsExitLevel(); err == nil {
		t.Fatal("invalid stored JSON decoded successfully")
	}
	if err := exits.MergeExitLevel(ExitLevel{Qty: 1}); err == nil {
		t.Fatal("invalid stored JSON merged successfully")
	}
}
