package tradeview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// These routes are deliberately independent of the generated operation manifest.
// A changed upstream contract requires review of the fixture expectations.
var _operationFixtures = []operationFixture{
	{"authorize", "POST", "/authorize"},
	{"cancelAllOrders", "POST", "/accounts/{accountId}/orders/cancelAll"},
	{"cancelOrder", "DELETE", "/accounts/{accountId}/orders/{orderId}"},
	{"closeIndividualPosition", "DELETE", "/accounts/{accountId}/individualPositions/{individualPositionId}"},
	{"closeNetPosition", "DELETE", "/accounts/{accountId}/netPositions/{netPositionId}"},
	{"closePosition", "DELETE", "/accounts/{accountId}/positions/{positionId}"},
	{"getAccounts", "GET", "/accounts"},
	{"getBalances", "GET", "/accounts/{accountId}/balances"},
	{"getConfiguration", "GET", "/config"},
	{"getDepth", "GET", "/depth"},
	{"getExecutions", "GET", "/accounts/{accountId}/executions"},
	{"getGroups", "GET", "/groups"},
	{"getHistory", "GET", "/history"},
	{"getIndividualPositions", "GET", "/accounts/{accountId}/individualPositions"},
	{"getInstruments", "GET", "/accounts/{accountId}/instruments"},
	{"getLeverage", "POST", "/accounts/{accountId}/getLeverage"},
	{"getMapping", "GET", "/mapping"},
	{"getNetPositions", "GET", "/accounts/{accountId}/netPositions"},
	{"getOrders", "GET", "/accounts/{accountId}/orders"},
	{"getOrdersHistory", "GET", "/accounts/{accountId}/ordersHistory"},
	{"getPermissions", "GET", "/permissions"},
	{"getPositions", "GET", "/accounts/{accountId}/positions"},
	{"getQuotes", "GET", "/quotes"},
	{"getState", "GET", "/accounts/{accountId}/state"},
	{"getSymbolInfo", "GET", "/symbol_info"},
	{"getUserInfo", "GET", "/userInfo"},
	{"logout", "POST", "/logout"},
	{"modifyIndividualPosition", "PUT", "/accounts/{accountId}/individualPositions/{individualPositionId}"},
	{"modifyOrder", "PUT", "/accounts/{accountId}/orders/{orderId}"},
	{"modifyPosition", "PUT", "/accounts/{accountId}/positions/{positionId}"},
	{"orderInfo", "POST", "/accounts/{accountId}/orderInfo"},
	{"placeOrder", "POST", "/accounts/{accountId}/orders"},
	{"postPromotionReport", "POST", "/promotionReport"},
	{"previewClosePosition", "POST", "/accounts/{accountId}/previewClosePosition"},
	{"previewLeverage", "POST", "/accounts/{accountId}/previewLeverage"},
	{"previewOrder", "POST", "/accounts/{accountId}/previewOrder"},
	{"setLeverage", "POST", "/accounts/{accountId}/setLeverage"},
	{"streamDepth", "GET", "/stream/depth"},
	{"streamIndividualPositions", "GET", "/accounts/{accountId}/stream/individualPositions"},
	{"streamNetPositions", "GET", "/accounts/{accountId}/stream/netPositions"},
	{"streamNotifications", "GET", "/accounts/{accountId}/stream/notifications"},
	{"streamOrders", "GET", "/accounts/{accountId}/stream/orders"},
	{"streamPositions", "GET", "/accounts/{accountId}/stream/positions"},
	{"streamQuotes", "GET", "/stream/quotes"},
	{"streamState", "GET", "/accounts/{accountId}/stream/state"},
	{"streaming", "GET", "/streaming"},
	{"trackLatency", "POST", "/accounts/{accountId}/trackLatency"},
}

type operationFixture struct{ id, method, path string }
type operationEntry struct {
	ID          string `json:"operationId"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	RawMethod   string `json:"rawMethod"`
	TypedMethod string `json:"typedMethod"`
	Streaming   bool   `json:"streaming"`
	Test        string `json:"test"`
}
type fixtureContract struct {
	source     map[string]any
	operations map[string]map[string]any
	manifest   map[string]operationEntry
}

func loadFixtureContract(t *testing.T) fixtureContract {
	t.Helper()
	data, err := os.ReadFile("../../api/broker-rest-api.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var source map[string]any
	if err := yaml.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	result := fixtureContract{source: source, operations: make(map[string]map[string]any, len(operationFixtures)), manifest: make(map[string]operationEntry, len(operationFixtures))}
	for _, pathValue := range source["paths"].(map[string]any) {
		for _, operationValue := range pathValue.(map[string]any) {
			operation, ok := operationValue.(map[string]any)
			if !ok {
				continue
			}
			id, ok := operation["operationId"].(string)
			if ok {
				result.operations[id] = operation
			}
		}
	}
	data, err = os.ReadFile("../../api/operations.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []operationEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if _, exists := result.manifest[entry.ID]; exists {
			t.Fatalf("duplicate operation %q", entry.ID)
		}
		result.manifest[entry.ID] = entry
	}
	return result
}

func TestOperationInventory(t *testing.T) {
	contract := loadFixtureContract(t)
	paths := contract.source["paths"].(map[string]any)
	if len(paths) != 43 || len(contract.operations) != 47 || len(contract.manifest) != 47 || len(_operationFixtures) != 47 {
		t.Fatalf("coverage count mismatch: paths=%d operations=%d manifest=%d fixtures=%d", len(paths), len(contract.operations), len(contract.manifest), len(_operationFixtures))
	}
	rawType := reflect.TypeOf((*Client)(nil))
	typedType := reflect.TypeOf((*ClientWithResponses)(nil))
	seen := make(map[string]bool, len(operationFixtures))
	finite, streams := 0, 0
	for _, fixture := range _operationFixtures {
		if seen[fixture.id] {
			t.Fatalf("duplicate fixture %q", fixture.id)
		}
		seen[fixture.id] = true
		operation, exists := contract.operations[fixture.id]
		if !exists {
			t.Fatalf("fixture %q missing upstream", fixture.id)
		}
		pathOperation := paths[fixture.path].(map[string]any)[strings.ToLower(fixture.method)].(map[string]any)
		if pathOperation["operationId"] != fixture.id {
			t.Errorf("%s does not match its upstream route", fixture.id)
		}
		entry, exists := contract.manifest[fixture.id]
		if !exists {
			t.Fatalf("fixture %q missing manifest", fixture.id)
		}
		methodName := strings.ToUpper(fixture.id[:1]) + fixture.id[1:]
		mediaType, _ := contract.requestBody(operation)
		if mediaType == "application/x-www-form-urlencoded" {
			methodName += "WithFormdataBody"
		}
		streaming := strings.HasPrefix(fixture.id, "stream")
		expectedTyped := methodName + "WithResponse"
		if streaming {
			expectedTyped = "Open" + methodName
		}
		if entry.Method != fixture.method || entry.Path != fixture.path || entry.RawMethod != methodName || entry.TypedMethod != expectedTyped || entry.Streaming != streaming || entry.Test != "TestOperationTransport/"+fixture.id {
			t.Errorf("%s manifest mismatch: %+v", fixture.id, entry)
		}
		if _, exists := rawType.MethodByName(methodName); !exists {
			t.Errorf("missing raw method %s", methodName)
		}
		if streaming {
			streams++
			if _, exists := rawType.MethodByName(expectedTyped); !exists {
				t.Errorf("missing stream helper %s", expectedTyped)
			}
			if _, exists := typedType.MethodByName(methodName + "WithResponse"); exists {
				t.Errorf("stream %s has a finite response reader", methodName)
			}
		} else {
			finite++
			if _, exists := typedType.MethodByName(expectedTyped); !exists {
				t.Errorf("missing typed method %s", expectedTyped)
			}
		}
	}
	if finite != 38 || streams != 9 {
		t.Fatalf("wrong reader split: finite=%d streams=%d", finite, streams)
	}
}

type fixtureContextKey struct{}

func TestOperationTransport(t *testing.T) {
	contract := loadFixtureContract(t)
	for _, fixture := range _operationFixtures {
		t.Run(fixture.id, func(t *testing.T) {
			operation := contract.operations[fixture.id]
			entry := contract.manifest[fixture.id]
			querySample, expectedQuery := contract.queryFixture(fixture.id, operation)
			mediaType, bodySample := contract.requestBody(operation)
			expectedPath := "/fixture-api" + fixture.path
			for _, match := range fixturePathNames(fixture.path) {
				expectedPath = strings.ReplaceAll(expectedPath, "{"+match+"}", "fixture-"+match)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != fixture.method {
					t.Errorf("method: got %s want %s", r.Method, fixture.method)
				}
				if r.URL.Path != expectedPath {
					t.Errorf("path: got %s want %s", r.URL.Path, expectedPath)
				}
				if !reflect.DeepEqual(r.URL.Query(), expectedQuery) {
					t.Errorf("query: got %v want %v", r.URL.Query(), expectedQuery)
				}
				if r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Errorf("missing bearer token")
				}
				if r.Header.Get("X-Fixture-Context") != fixture.id {
					t.Errorf("request context did not reach request editor")
				}
				assertFixtureBody(t, r, mediaType, bodySample)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"s":"ok"}`)
			}))
			defer server.Close()
			client, err := NewClient(server.URL+"/fixture-api", WithBearerToken("fixture-token"), WithRequestEditorFn(func(ctx context.Context, r *http.Request) error {
				id, _ := ctx.Value(fixtureContextKey{}).(string)
				r.Header.Set("X-Fixture-Context", id)
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(context.Background(), fixtureContextKey{}, fixture.id)
			results := invokeFixture(t, reflect.ValueOf(client).MethodByName(entry.RawMethod), ctx, fixture.path, querySample, bodySample)
			if !results[1].IsNil() {
				t.Fatal(results[1].Interface().(error))
			}
			response := results[0].Interface().(*http.Response)
			data, err := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("response read/close: %v / %v", err, closeErr)
			}
			if response.StatusCode != http.StatusOK || string(data) != `{"s":"ok"}` {
				t.Errorf("unexpected raw response: %d %s", response.StatusCode, data)
			}
			if calls.Load() != 1 {
				t.Errorf("got %d requests, want one", calls.Load())
			}
		})
	}
}

func TestFiniteResponseBindings(t *testing.T) {
	contract := loadFixtureContract(t)
	for _, fixture := range _operationFixtures {
		entry := contract.manifest[fixture.id]
		if entry.Streaming {
			continue
		}
		t.Run(fixture.id, func(t *testing.T) {
			querySample, _ := contract.queryFixture(fixture.id, contract.operations[fixture.id])
			_, bodySample := contract.requestBody(contract.operations[fixture.id])
			for _, responseBody := range []string{`{"s":"ok"}`, `{"s":"error","errtype":"forbidden","errmsg":"fixture denied"}`} {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, responseBody)
				}))
				client, err := NewClientWithResponses(server.URL)
				if err != nil {
					server.Close()
					t.Fatal(err)
				}
				results := invokeFixture(t, reflect.ValueOf(client).MethodByName(entry.TypedMethod), context.Background(), fixture.path, querySample, bodySample)
				server.Close()
				if strings.Contains(responseBody, `"s":"error"`) {
					if results[1].IsNil() {
						t.Fatal("HTTP-200 API error was not returned")
					}
					apiErr, ok := results[1].Interface().(*APIError)
					if !ok || apiErr.Code != "forbidden" || apiErr.Message != "fixture denied" || apiErr.StatusCode != http.StatusOK {
						t.Fatalf("wrong API error: %v", results[1].Interface())
					}
					continue
				}
				if !results[1].IsNil() {
					t.Fatal(results[1].Interface().(error))
				}
				response := results[0].Elem()
				if !bytes.Equal(response.FieldByName("Body").Bytes(), []byte(responseBody)) {
					t.Error("typed response lost raw body")
				}
				jsonBody := response.FieldByName("JSON200")
				if !jsonBody.IsValid() || jsonBody.IsNil() {
					t.Error("typed response was not decoded")
				}
			}
		})
	}
}

func fixturePathNames(path string) []string {
	matches := regexp.MustCompile(`\{([^}]+)\}`).FindAllStringSubmatch(path, -1)
	result := make([]string, len(matches))
	for i, match := range matches {
		result[i] = match[1]
	}
	return result
}

func invokeFixture(t *testing.T, method reflect.Value, ctx context.Context, path string, params, body any) []reflect.Value {
	t.Helper()
	if !method.IsValid() {
		t.Fatal("missing endpoint method")
	}
	typ := method.Type()
	args := []reflect.Value{reflect.ValueOf(ctx)}
	pathNames := fixturePathNames(path)
	pathIndex := 0
	for i := 1; i < typ.NumIn(); i++ {
		argType := typ.In(i)
		if typ.IsVariadic() && i == typ.NumIn()-1 {
			args = append(args, reflect.MakeSlice(argType, 0, 0))
			continue
		}
		if argType.Kind() == reflect.String && pathIndex < len(pathNames) {
			arg := reflect.New(argType).Elem()
			arg.SetString("fixture-" + pathNames[pathIndex])
			pathIndex++
			args = append(args, arg)
			continue
		}
		sample := body
		if argType.Kind() == reflect.Pointer && strings.HasSuffix(argType.Elem().Name(), "Params") {
			sample = params
		}
		arg := reflect.New(argType)
		data, err := json.Marshal(sample)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, arg.Interface()); err != nil {
			t.Fatalf("build %s argument from %s: %v", argType, data, err)
		}
		args = append(args, arg.Elem())
	}
	if pathIndex != len(pathNames) {
		t.Fatalf("did not supply all path arguments: %d/%d", pathIndex, len(pathNames))
	}
	return method.CallSlice(args)
}

func (c fixtureContract) resolve(value map[string]any) map[string]any {
	for {
		ref, ok := value["$ref"].(string)
		if !ok {
			return value
		}
		var node any = c.source
		for _, key := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			node = node.(map[string]any)[strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")]
		}
		value = node.(map[string]any)
	}
}

func (c fixtureContract) queryFixture(id string, operation map[string]any) (map[string]any, url.Values) {
	parameters, _ := operation["parameters"].([]any)
	values := make(map[string]any, len(parameters))
	expected := make(url.Values, len(parameters))
	for _, parameterValue := range parameters {
		parameter := c.resolve(parameterValue.(map[string]any))
		if parameter["in"] != "query" {
			continue
		}
		name := parameter["name"].(string)
		schema := parameter["schema"].(map[string]any)
		sample := c.sample(schema, 0)
		if name == "locale" {
			sample = "en"
		}
		if id == "getInstruments" && name == "symbols" {
			sample = []any{"EURUSD", "USDJPY"}
		}
		values[name] = sample
		if array, ok := sample.([]any); ok {
			parts := make([]string, len(array))
			for i, item := range array {
				parts[i] = fixtureScalar(item)
			}
			if parameter["explode"] == false || (id == "getInstruments" && name == "symbols") {
				expected.Set(name, strings.Join(parts, ","))
			} else {
				expected[name] = parts
			}
		} else {
			expected.Set(name, fixtureScalar(sample))
		}
	}
	return values, expected
}

func (c fixtureContract) requestBody(operation map[string]any) (string, any) {
	body, ok := operation["requestBody"].(map[string]any)
	if !ok {
		return "", nil
	}
	body = c.resolve(body)
	content := body["content"].(map[string]any)
	for _, mediaType := range []string{"application/x-www-form-urlencoded", "application/json"} {
		if item, ok := content[mediaType].(map[string]any); ok {
			return mediaType, c.sample(item["schema"].(map[string]any), 0)
		}
	}
	panic("unexpected request media type")
}

func (c fixtureContract) sample(schema map[string]any, depth int) any {
	if depth > 12 {
		return nil
	}
	schema = c.resolve(schema)
	if example, exists := schema["example"]; exists {
		// A few upstream string fields have numeric documentation examples.
		// Honor the declared wire type when constructing a request fixture.
		if schema["type"] == "string" {
			return fixtureScalar(example)
		}
		return example
	}
	if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if variants, ok := schema[key].([]any); ok && len(variants) > 0 {
			return c.sample(variants[0].(map[string]any), depth+1)
		}
	}
	if variants, ok := schema["allOf"].([]any); ok {
		merged := make(map[string]any)
		for _, variant := range variants {
			if value, ok := c.sample(variant.(map[string]any), depth+1).(map[string]any); ok {
				for key, item := range value {
					merged[key] = item
				}
			}
		}
		return merged
	}
	switch schema["type"] {
	case "number":
		return 12.75
	case "integer":
		return 7
	case "boolean":
		return true
	case "array":
		if items, ok := schema["items"].(map[string]any); ok {
			return []any{c.sample(items, depth+1)}
		}
		return []any{"fixture-array"}
	case "string":
		return "fixture value/+?&"
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		result := make(map[string]any, len(properties))
		keys := make([]string, 0, len(properties))
		for key := range properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			result[key] = c.sample(properties[key].(map[string]any), depth+1)
		}
		return result
	}
	return make(map[string]any)
}

func fixtureScalar(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(value)
	default:
		return fmt.Sprint(value)
	}
}

func fixtureForm(sample any) url.Values {
	object := sample.(map[string]any)
	expected := make(url.Values, len(object))
	for key, value := range object {
		switch value := value.(type) {
		case []any:
			scalar := true
			for _, item := range value {
				switch item.(type) {
				case map[string]any, []any:
					scalar = false
				}
			}
			if scalar {
				for _, item := range value {
					expected.Add(key, fixtureScalar(item))
				}
				continue
			}
			data, _ := json.Marshal(value)
			expected.Set(key, string(data))
		case map[string]any:
			data, _ := json.Marshal(value)
			expected.Set(key, string(data))
		default:
			expected.Set(key, fixtureScalar(value))
		}
	}
	return expected
}

func assertFixtureBody(t *testing.T, r *http.Request, mediaType string, sample any) {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Error(err)
		return
	}
	if mediaType == "" {
		if len(body) != 0 {
			t.Errorf("unexpected request body %s", body)
		}
		return
	}
	actualMediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || actualMediaType != mediaType {
		t.Errorf("content type: got %q want %q", r.Header.Get("Content-Type"), mediaType)
	}
	if mediaType == "application/x-www-form-urlencoded" {
		actual, err := url.ParseQuery(string(body))
		if err != nil {
			t.Error(err)
			return
		}
		expected := fixtureForm(sample)
		if !reflect.DeepEqual(actual, expected) {
			t.Errorf("form: got %v want %v", actual, expected)
		}
	} else {
		var actual, want any
		if err := json.Unmarshal(body, &actual); err != nil {
			t.Error(err)
			return
		}
		data, _ := json.Marshal(sample)
		if err := json.Unmarshal(data, &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, want) {
			t.Errorf("JSON body: got %s want %s", body, data)
		}
	}
}
