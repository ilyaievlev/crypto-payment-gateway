package tron

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	cryptovaultv1 "github.com/renegadik/crypto-payment-gateway/pkg/proto/cryptovault/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestNewClientRejectsInvalidEndpoint проверяет обязательные ограничения адреса узла.
func TestNewClientRejectsInvalidEndpoint(t *testing.T) {
	for _, endpoint := range []string{"", "nile.trongrid.io", "file:///tmp/node", "https://node.test/path?token=x", "http://remote.test"} {
		t.Run(endpoint, func(t *testing.T) {
			_, err := NewClient(endpoint, "")
			require.Error(t, err)
		})
	}
}

// TestSolidifiedBlockMethods проверяет запросы к финализированной цепочке.
func TestSolidifiedBlockMethods(t *testing.T) {
	client, err := NewClient("http://127.0.0.1", "")
	require.NoError(t, err)
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case latestSolidifiedBlockPath:
			return jsonResponse(http.StatusOK, `{"blockID":"`+strings.Repeat("0", 64)+`","block_header":{"raw_data":{"number":77}}}`), nil
		case solidifiedBlockByNumberPath:
			var request struct {
				Number uint64 `json:"num"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			require.Equal(t, uint64(76), request.Number)
			blockID := make([]byte, 32)
			blockID[7] = 76
			return jsonResponse(http.StatusOK, fmt.Sprintf(`{"blockID":%q,"block_header":{"raw_data":{"number":76}}}`, hex.EncodeToString(blockID))), nil
		default:
			t.Fatalf("неожиданный endpoint %s", r.URL.Path)
			return nil, nil
		}
	})
	height, err := client.SolidifiedHeight(context.Background())
	require.NoError(t, err)
	require.Equal(t, uint64(77), height)
	block, err := client.SolidifiedBlock(context.Background(), 76)
	require.NoError(t, err)
	require.Equal(t, uint64(76), block.Number)
	require.Len(t, block.ID, 64)
	require.NotEmpty(t, block.Payload)
	_, err = client.SolidifiedBlock(context.Background(), 0)
	require.Error(t, err)
}

// TestBroadcastRequiresNodeAcceptance проверяет успешный и отклонённый broadcast.
func TestBroadcastRequiresNodeAcceptance(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
		fail bool
	}{
		{name: "accepted", body: `{"result":true,"txid":"` + strings.Repeat("ab", 32) + `"}`, want: strings.Repeat("ab", 32)},
		{name: "rejected", body: `{"result":false,"code":"SIGERROR"}`, fail: true},
		{name: "missing tx id", body: `{"result":true}`, fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient("http://127.0.0.1", "")
			require.NoError(t, err)
			client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, broadcastHexPath, r.URL.Path)
				var request map[string]string
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				require.Equal(t, "7b227369676e6564223a747275657d", request["transaction"])
				return jsonResponse(http.StatusOK, tt.body), nil
			})
			id, err := client.Broadcast(context.Background(), []byte(`{"signed":true}`))
			if tt.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, id)
		})
	}
}

// TestSolidifiedTransactionResponses различает ожидание и финализированную запись.
func TestSolidifiedTransactionResponses(t *testing.T) {
	id := strings.Repeat("cd", 32)
	client, err := NewClient("http://127.0.0.1", "")
	require.NoError(t, err)
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case solidifiedTransactionPath:
			return jsonResponse(http.StatusOK, `{"txID":"`+id+`","raw_data":{}}`), nil
		case solidifiedTransactionInfoPath:
			return jsonResponse(http.StatusOK, `{"id":"`+id+`","result":"SUCCESS","receipt":{"result":"SUCCESS"}}`), nil
		default:
			t.Fatalf("неожиданный endpoint %s", r.URL.Path)
			return nil, nil
		}
	})
	body, found, err := client.SolidifiedTransaction(context.Background(), id)
	require.NoError(t, err)
	require.True(t, found)
	require.NotEmpty(t, body)
	info, found, err := client.SolidifiedTransactionInfo(context.Background(), id)
	require.NoError(t, err)
	require.True(t, found)
	require.Contains(t, string(info), `"SUCCESS"`)
	_, _, err = client.SolidifiedTransaction(context.Background(), "bad-id")
	require.Error(t, err)
}

// TestLatestContext проверяет запрос к узлу и преобразование данных блока.
func TestLatestContext(t *testing.T) {
	blockID := make([]byte, 32)
	blockID[7] = 0x2a
	for i := 8; i < len(blockID); i++ {
		blockID[i] = byte(i)
	}

	client, err := NewClient("http://127.0.0.1", "test-key")
	require.NoError(t, err)
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, latestBlockPath, r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "test-key", r.Header.Get("TRON-PRO-API-KEY"))
		body := fmt.Sprintf(
			`{"blockID":%q,"block_header":{"raw_data":{"number":42,"timestamp":1791413298000}}}`,
			hex.EncodeToString(blockID),
		)
		return jsonResponse(http.StatusOK, body), nil
	})
	encoded, err := client.LatestContext(context.Background(), 100_000_000)
	require.NoError(t, err)

	var got cryptovaultv1.TronContextV1
	require.NoError(t, proto.Unmarshal(encoded, &got))
	require.Equal(t, uint64(42), got.BlockNumber)
	require.Equal(t, blockID, got.BlockId)
	require.Equal(t, int64(100_000_000), got.FeeLimitSun)
	require.InDelta(t, time.Now().Add(time.Minute).UnixMilli(), got.ExpirationMs, 2_000)
}

// TestNileConnectivity проверяет доступ к Nile, если задан флаг live-тестирования.
func TestNileConnectivity(t *testing.T) {
	if os.Getenv("TRON_NILE_INTEGRATION") != "1" {
		t.Skip("установите TRON_NILE_INTEGRATION=1 для запроса к публичной Nile")
	}

	endpoint := os.Getenv("TRON_NILE_ENDPOINT")
	if endpoint == "" {
		endpoint = nileEndpoint
	}
	client, err := NewClient(endpoint, os.Getenv("TRON_NILE_API_KEY"))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	encoded, err := client.LatestContext(ctx, 0)
	require.NoError(t, err)

	var got cryptovaultv1.TronContextV1
	require.NoError(t, proto.Unmarshal(encoded, &got))
	require.Positive(t, got.BlockNumber)
	require.Len(t, got.BlockId, 32)
	finalizedHeight, err := client.SolidifiedHeight(ctx)
	require.NoError(t, err)
	require.Positive(t, finalizedHeight)
	block, err := client.SolidifiedBlock(ctx, finalizedHeight)
	require.NoError(t, err)
	require.Equal(t, finalizedHeight, block.Number)
	require.Len(t, block.ID, 64)
}

// TestLatestContextRejectsNodeErrors проверяет HTTP-ошибки и неправильный ответ узла.
func TestLatestContextRejectsNodeErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "HTTP error", statusCode: http.StatusBadGateway, body: `{}`},
		{name: "malformed JSON", statusCode: http.StatusOK, body: `not-json`},
		{name: "invalid block ID", statusCode: http.StatusOK, body: `{"blockID":"bad"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient("http://127.0.0.1", "")
			require.NoError(t, err)
			client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return jsonResponse(tt.statusCode, tt.body), nil
			})
			_, err = client.LatestContext(context.Background(), 0)
			require.Error(t, err)
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip подменяет HTTP transport в unit-тестах без открытия сетевого порта.
func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
