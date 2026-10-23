//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestCodexTimezoneDiagnosticLog(t *testing.T) {
	for _, tc := range []struct {
		name, accountTimezone, globalTimezone, clientText, wantTimezone string
		wantRewritten, wantError                                        bool
	}{
		{"account override", "Asia/Taipei", "UTC", codexTimezoneTestContext, "Asia/Taipei", true, false},
		{"inherit global", "", "Asia/Taipei", codexTimezoneTestContext, "Asia/Taipei", true, false},
		{"account preserves client", "off", "Asia/Taipei", codexTimezoneTestContext, "", false, false},
		{"global preserves client", "", "", codexTimezoneTestContext, "", false, false},
		{"already matching", "Asia/Taipei", "", "<environment_context><timezone>Asia/Taipei</timezone></environment_context>", "Asia/Taipei", false, false},
		{"missing context", "Asia/Taipei", "", "private prompt", "Asia/Taipei", false, false},
		{"rewrite fails", "invalid-zone", "", codexTimezoneTestContext, "invalid-zone", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetGatewayForwardingSettingsCacheForTest(t)
			core, logs := observer.New(zap.InfoLevel)
			ctx := logger.IntoContext(context.Background(), zap.New(core).With(zap.String("request_id", "req-timezone")))
			svc := &OpenAIGatewayService{settingService: NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{
				SettingKeyOpenAICodexTimezone: tc.globalTimezone,
			}}, &config.Config{})}
			account := &Account{
				ID: 112, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "private-token"},
				Extra:       map[string]any{codexTimezoneExtraKey: tc.accountTimezone},
			}
			body, err := json.Marshal(map[string]any{
				"input":    tc.clientText,
				"metadata": map[string]string{"private": "private-metadata"},
			})
			require.NoError(t, err)
			out, err := svc.applyCodexTimezone(ctx, account, body)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tc.wantRewritten {
					require.Contains(t, gjson.GetBytes(out, "input").String(), "<timezone>Asia/Taipei</timezone>")
				} else {
					require.Equal(t, body, out)
				}
			}
			require.Len(t, logs.All(), 1)
			entry := logs.All()[0]
			if tc.wantError {
				require.Equal(t, "openai.codex_timezone_failed", entry.Message)
				require.Equal(t, zap.WarnLevel, entry.Level)
			} else {
				require.Equal(t, "openai.codex_timezone", entry.Message)
				require.Equal(t, zap.InfoLevel, entry.Level)
			}
			// Only policy metadata and inherited request attribution may be logged.
			require.Equal(t, map[string]any{
				"request_id": "req-timezone", "account_id": int64(112),
				"timezone": tc.wantTimezone, "rewritten": tc.wantRewritten,
			}, entry.ContextMap())
		})
	}
}

func TestCodexTimezoneDiagnosticLogSkipsOtherAccountTypes(t *testing.T) {
	for _, account := range []*Account{
		nil,
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
	} {
		core, logs := observer.New(zap.InfoLevel)
		ctx := logger.IntoContext(context.Background(), zap.New(core))
		body := []byte(`{"input":"private prompt"}`)
		out, err := (&OpenAIGatewayService{}).applyCodexTimezone(ctx, account, body)
		require.NoError(t, err)
		require.Equal(t, body, out)
		require.Empty(t, logs.All())
	}
}
