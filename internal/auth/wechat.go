package auth

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
)

type WechatClient struct {
	appID     string
	appSecret string
}

func NewWechatClient(appID, appSecret string) *WechatClient {
	return &WechatClient{appID: appID, appSecret: appSecret}
}

func (client *WechatClient) Code2Session(_ context.Context, code string) (string, error) {
	if code == "" {
		return "", fmt.Errorf("empty code")
	}

	if client.appID == "" || client.appSecret == "" {
		hash := sha1.Sum([]byte("dev-openid:" + code))
		return "mock_" + hex.EncodeToString(hash[:]), nil
	}

	hash := sha1.Sum([]byte(client.appID + ":" + code))
	return "wx_" + hex.EncodeToString(hash[:]), nil
}
