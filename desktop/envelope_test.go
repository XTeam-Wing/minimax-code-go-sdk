package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBusinessEnvelope(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		code    string
		message string
		status  int64
		raw     string
		fail    bool
		size    bool
	}{
		{
			name:    "status code 1",
			body:    `{"base_resp":{"status_code":1,"status_msg":"memory writes are disabled","code":"MEMORY_DISABLED"}}`,
			code:    "MEMORY_DISABLED",
			message: "memory writes are disabled",
			status:  1,
			fail:    true,
		},
		{
			name:    "decimal status string",
			body:    `{"base_resp":{"status_code":"1","status_msg":"old_string matched more than once","code":"OLD_STRING_AMBIGUOUS"}}`,
			code:    "OLD_STRING_AMBIGUOUS",
			message: "old_string matched more than once",
			status:  1,
			fail:    true,
		},
		{
			name: "status zero",
			body: `{"new_size_bytes":4,"base_resp":{"status_code":0,"status_msg":"OK"}}`,
			size: true,
		},
		{
			name: "status zero string",
			body: `{"new_size_bytes":4,"base_resp":{"status_code":"0","status_msg":"OK"}}`,
			size: true,
		},
		{
			name: "missing envelope",
			body: `{"new_size_bytes":4}`,
			size: true,
		},
		{
			name: "null envelope",
			body: `{"new_size_bytes":4,"base_resp":null}`,
			size: true,
		},
		{
			name: "envelope array",
			body: `{"base_resp":[]}`,
		},
		{
			name:    "fractional status",
			body:    `{"base_resp":{"status_code":0.5,"status_msg":"fraction","code":"X"}}`,
			code:    "X",
			message: "fraction",
			raw:     "0.5",
			fail:    true,
		},
		{
			name: "fractional zero",
			body: `{"new_size_bytes":4,"base_resp":{"status_code":0.0,"status_msg":"OK"}}`,
			size: true,
		},
		{
			name:    "int64 overflow",
			body:    `{"base_resp":{"status_code":9223372036854775808,"status_msg":"overflow","code":"OVER"}}`,
			code:    "OVER",
			message: "overflow",
			raw:     "9223372036854775808",
			fail:    true,
		},
		{
			name:    "exact int64 max",
			body:    `{"base_resp":{"status_code":9223372036854775807,"status_msg":"edge","code":"EDGE"}}`,
			code:    "EDGE",
			message: "edge",
			status:  9223372036854775807,
			fail:    true,
		},
		{
			name: "unsupported status",
			body: `{"base_resp":{"status_code":"nope","status_msg":"ignored"}}`,
		},
		{
			name: "status object",
			body: `{"base_resp":{"status_code":{"n":1}}}`,
		},
		{
			name: "foreign status",
			body: `{"status_code":1,"new_size_bytes":4}`,
			size: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := New(bodyRoundTrip{body: []byte(tc.body)})
			got, err := client.AppendMemory(context.Background(), AppendMemoryReq{Content: "x"})
			if tc.fail {
				var biz *BusinessError
				if !errors.As(err, &biz) {
					t.Fatalf("error %#v", err)
				}
				if biz.Code != tc.code || biz.Message != tc.message {
					t.Fatalf("business %#v", biz)
				}
				if tc.status != 0 && biz.StatusCode != tc.status {
					t.Fatalf("status %d", biz.StatusCode)
				}
				if tc.raw != "" && biz.RawStatus != tc.raw {
					t.Fatalf("raw status %q", biz.RawStatus)
				}
				if tc.raw != "" && biz.StatusCode != 0 {
					t.Fatalf("status %d", biz.StatusCode)
				}
				if !strings.Contains(string(biz.Body), tc.code) {
					t.Fatalf("body %s", biz.Body)
				}
				if got.BaseResp == nil {
					t.Fatal("decoded base_resp was dropped")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.size {
				n, ok := got.NewSizeBytes.Float64()
				if !ok || n != 4 {
					t.Fatalf("size %#v", got.NewSizeBytes)
				}
			}
		})
	}
}

type bodyRoundTrip struct {
	body []byte
}

func (b bodyRoundTrip) RoundTrip(context.Context, HTTPRequest) (HTTPResponse, error) {
	return HTTPResponse{Status: 200, Body: b.body}, nil
}

func (b bodyRoundTrip) RoundTripStream(context.Context, HTTPRequest) (*HTTPStream, error) {
	return nil, errors.New("desktop: no stream")
}
