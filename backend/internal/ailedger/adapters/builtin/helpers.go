package builtin

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/themisto/backend/internal/ailedger"
)

type adapter struct {
	descriptor ailedger.ConnectorDescriptor
	validate   func(ailedger.Credentials, ailedger.ConnectorConfig) error
	sync       func(context.Context, ailedger.Checkpoint, ailedger.FactSink) (ailedger.Checkpoint, error)
}

func (a adapter) Descriptor() ailedger.ConnectorDescriptor { return a.descriptor }
func (a adapter) Validate(credentials ailedger.Credentials, config ailedger.ConnectorConfig) error {
	if a.validate == nil {
		return nil
	}
	return a.validate(credentials, config)
}
func (a adapter) Sync(ctx context.Context, checkpoint ailedger.Checkpoint, sink ailedger.FactSink) (ailedger.Checkpoint, error) {
	return a.sync(ctx, checkpoint, sink)
}

type normalizedFactDocument struct {
	Products   []ailedger.ProductFact          `json:"products"`
	Identities []ailedger.ExternalIdentityFact `json:"identities"`
	Licenses   []ailedger.LicenseFact          `json:"licenses"`
	Metrics    []ailedger.MetricFact           `json:"metrics"`
	Costs      []ailedger.CostFact             `json:"costs"`
	NextPage   string                          `json:"next_page,omitempty"`
}

func emitDocument(document normalizedFactDocument, sink ailedger.FactSink) error {
	for _, fact := range document.Products {
		if err := sink.UpsertProduct(fact); err != nil {
			return err
		}
	}
	for _, fact := range document.Identities {
		if err := sink.UpsertIdentity(fact); err != nil {
			return err
		}
	}
	for _, fact := range document.Licenses {
		if err := sink.UpsertLicense(fact); err != nil {
			return err
		}
	}
	for _, fact := range document.Metrics {
		if err := sink.UpsertMetric(fact); err != nil {
			return err
		}
	}
	for _, fact := range document.Costs {
		if err := sink.UpsertCost(fact); err != nil {
			return err
		}
	}
	return nil
}

func decodeConfiguredDocument(config ailedger.ConnectorConfig) (normalizedFactDocument, bool, error) {
	value, ok := config["normalized_facts"]
	if !ok {
		return normalizedFactDocument{}, false, nil
	}
	raw, err := json.Marshal(value)
	if text, ok := value.(string); ok {
		raw = []byte(text)
	}
	if err != nil {
		return normalizedFactDocument{}, true, err
	}
	var document normalizedFactDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return normalizedFactDocument{}, true, err
	}
	return document, true, nil
}

func requireString(values map[string]interface{}, key string) (string, error) {
	value, _ := values[key].(string)
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func authenticatedGET(ctx context.Context, target, header, token string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set(header, token)
	return ailedger.HTTPClientFromContext(ctx).Do(request)
}

type HTTPStatusError struct {
	Code int
	Body string
}

func (e *HTTPStatusError) Error() string   { return fmt.Sprintf("connector HTTP %d: %s", e.Code, e.Body) }
func (e *HTTPStatusError) StatusCode() int { return e.Code }

func decodeResponse(response *http.Response, out interface{}) error {
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return &HTTPStatusError{Code: response.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	return json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(out)
}

func stringValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

func unixTime(value interface{}, fallback time.Time) time.Time {
	switch typed := value.(type) {
	case float64:
		return time.Unix(int64(typed), 0).UTC()
	case json.Number:
		if seconds, err := typed.Int64(); err == nil {
			return time.Unix(seconds, 0).UTC()
		}
	case string:
		if parsed, err := time.Parse(time.RFC3339, typed); err == nil {
			return parsed.UTC()
		}
	}
	return fallback.UTC()
}

func period(start, end time.Time) (*time.Time, *time.Time) {
	return &start, &end
}

func parseCSV(raw string) ([][]string, error) {
	reader := csv.NewReader(bytes.NewBufferString(raw))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	return reader.ReadAll()
}
