package mschatbot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	tenantID     string
	clientID     string
	clientSecret string

	targetEmail string
	teamsAppID  string
	message     string
}

func NewClient(tenantID, clientID, clientSecret, targetEmail, teamsAppID, message string) *Client {
	return &Client{
		tenantID:     tenantID,
		clientID:     clientID,
		clientSecret: clientSecret,
		targetEmail:  targetEmail,
		teamsAppID:   teamsAppID,
		message:      message,
	}
}

func (c *Client) GetBotToken() (string, error) {
	endpoint := "https://login.microsoftonline.com/" + c.tenantID + "/oauth2/v2.0/token"

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)
	data.Set("scope", "https://api.botframework.com/.default")

	resp, err := http.Post(endpoint, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("request bot token gagal: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	token, ok := result["access_token"].(string)
	if !ok {
		return "", fmt.Errorf("bot token tidak ditemukan: %s", string(body))
	}
	return token, nil
}

func (c *Client) GetGraphToken() (string, error) {
	endpoint := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", c.tenantID)

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)
	data.Set("scope", "https://graph.microsoft.com/.default")

	resp, err := http.Post(endpoint, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("request graph token gagal: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	token, ok := result["access_token"].(string)
	if !ok {
		return "", fmt.Errorf("graph token tidak ditemukan: %s", string(body))
	}
	return token, nil
}

func (c *Client) GetUserID(graphToken, email string) (string, error) {
	apiURL := fmt.Sprintf("https://graph.microsoft.com/v1.0/users/%s", email)

	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("Authorization", "Bearer "+graphToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var user map[string]interface{}
	json.Unmarshal(body, &user)

	id, ok := user["id"].(string)
	if !ok {
		return "", fmt.Errorf("user tidak ditemukan (%s): %s", email, string(body))
	}
	return id, nil
}

func (c *Client) InstallBot(graphToken, userID string) error {
	apiURL := fmt.Sprintf("https://graph.microsoft.com/v1.0/users/%s/teamwork/installedApps", userID)

	payload := map[string]interface{}{
		"teamsApp@odata.bind": fmt.Sprintf("https://graph.microsoft.com/v1.0/appCatalogs/teamsApps/%s", c.teamsAppID),
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", apiURL, bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+graphToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 201 = installed, 409 = sudah terinstall, keduanya ok
	if resp.StatusCode != 201 && resp.StatusCode != 409 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gagal install bot (status %d): %s", resp.StatusCode, string(respBody))
	}

	if resp.StatusCode == 409 {
		fmt.Println("ℹ️  Bot sudah terinstall sebelumnya, lanjut...")
	}

	return nil
}

func GetChatID(graphToken, userID string) (string, error) {
	// Step 1: Find the App Installation ID
	// We remove $expand=chat to satisfy the API's restriction
	filter := fmt.Sprintf("teamsApp/id eq '%s'", teamsAppID)
	apiURL := fmt.Sprintf(
		"https://graph.microsoft.com/v1.0/users/%s/teamwork/installedApps?$filter=%s",
		userID, url.QueryEscape(filter),
	)

	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("Authorization", "Bearer "+graphToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Value []struct {
			ID string `json:"id"`
		} `json:"value"`
	}
	json.Unmarshal(body, &result)

	if len(result.Value) == 0 {
		return "", fmt.Errorf("bot belum terinstall untuk user ini")
	}

	// This is the Installation ID (e.g., "MCMjY2...")
	appInstallationID := result.Value[0].ID

	// Step 2: Get the Chat ID using the Installation ID
	chatURL := fmt.Sprintf(
		"https://graph.microsoft.com/v1.0/users/%s/teamwork/installedApps/%s/chat",
		userID, appInstallationID,
	)

	reqChat, _ := http.NewRequest("GET", chatURL, nil)
	reqChat.Header.Set("Authorization", "Bearer "+graphToken)

	respChat, err := http.DefaultClient.Do(reqChat)
	if err != nil {
		return "", err
	}
	defer respChat.Body.Close()

	chatBody, _ := io.ReadAll(respChat.Body)
	var chatResult map[string]interface{}
	json.Unmarshal(chatBody, &chatResult)

	chatID, ok := chatResult["id"].(string)
	if !ok {
		return "", fmt.Errorf("gagal mendapatkan Chat ID: %s", string(chatBody))
	}

	return chatID, nil
}

func (c *Client) SendMessage() error {

	// Dapatkan bot token
	botToken, err := c.GetBotToken()
	if err != nil {
		return fmt.Errorf("gagal dapatkan bot token: %w", err)
	}

	// Dapatkan graph token
	graphToken, err := c.GetGraphToken()
	if err != nil {
		return fmt.Errorf("gagal dapatkan graph token: %w", err)
	}

	// Dapatkan user ID
	userID, err := c.GetUserID(graphToken, c.targetEmail)
	if err != nil {
		return fmt.Errorf("gagal dapatkan user ID: %w", err)
	}

	// Install bot ke user (jika belum)
	err = c.InstallBot(graphToken, userID)
	if err != nil {
		return fmt.Errorf("gagal install bot: %w", err)
	}

	// Dapatkan chat ID
	chatID, err := c.GetChatID(graphToken, userID)
	if err != nil {
		return fmt.Errorf("gagal dapatkan chat ID: %w", err)
	}

	// Kirim pesan
	text := c.message
	serviceURL := "https://smba.trafficmanager.net/apis"
	apiURL := fmt.Sprintf("%s/v3/conversations/%s/activities", serviceURL, chatID)

	payload := map[string]interface{}{
		"type": "message",
		"from": map[string]string{
			"id":   "28:" + c.clientID,
			"name": "NotifBot",
		},
		"text": text,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", apiURL, bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gagal kirim pesan (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// func (c *Client) SendMessage(botToken, chatID, text string) error {
// 	serviceURL := "https://smba.trafficmanager.net/apis"
// 	apiURL := fmt.Sprintf("%s/v3/conversations/%s/activities", serviceURL, chatID)

// 	payload := map[string]interface{}{
// 		"type": "message",
// 		"from": map[string]string{
// 			"id":   "28:" + c.clientID,
// 			"name": "NotifBot",
// 		},
// 		"text": text,
// 	}

// 	body, _ := json.Marshal(payload)
// 	req, _ := http.NewRequest("POST", apiURL, bytes.NewBuffer(body))
// 	req.Header.Set("Authorization", "Bearer "+botToken)
// 	req.Header.Set("Content-Type", "application/json")

// 	resp, err := http.DefaultClient.Do(req)
// 	if err != nil {
// 		return err
// 	}
// 	defer resp.Body.Close()

// 	respBody, _ := io.ReadAll(resp.Body)
// 	if resp.StatusCode >= 400 {
// 		return fmt.Errorf("gagal kirim pesan (status %d): %s", resp.StatusCode, string(respBody))
// 	}

// 	return nil
// }
