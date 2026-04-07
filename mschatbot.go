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

const (
	tenantID     = ""
	clientID     = ""
	clientSecret = ""
	// clientSecret = ""

	targetEmail = ""
	teamsAppID  = ""
	message     = "Halo! Izin Test Notifikasi ya mas 🚀"
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

func (c *Client) getBotToken() (string, error) {
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

func (c *Client) getGraphToken() (string, error) {
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

func (c *Client) getUserID(graphToken, email string) (string, error) {
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

func (c *Client) installBot(graphToken, userID string) error {
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

func (c *Client) getChatID(graphToken, userID string) (string, error) {
	apiURL := fmt.Sprintf(
		"https://graph.microsoft.com/v1.0/users/%s/teamwork/installedApps?$expand=chat&$filter=teamsApp/id eq '%s'",
		userID, c.teamsAppID,
	)

	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("Authorization", "Bearer "+graphToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	values, ok := result["value"].([]interface{})
	if !ok || len(values) == 0 {
		return "", fmt.Errorf("chat tidak ditemukan: %s", string(body))
	}

	firstApp, ok := values[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("format response tidak valid")
	}

	chat, ok := firstApp["chat"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("chat object tidak ditemukan, pastikan bot sudah di-install")
	}

	chatID, ok := chat["id"].(string)
	if !ok {
		return "", fmt.Errorf("chat ID tidak ditemukan")
	}

	return chatID, nil
}

func (c *Client) sendMessage(botToken, chatID, text string) error {
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

func main() {
	client := &Client{
		tenantID:     tenantID,
		clientID:     clientID,
		clientSecret: clientSecret,
		targetEmail:  targetEmail,
		teamsAppID:   teamsAppID,
		message:      message,
	}

	fmt.Println("📡 Mengambil bot token...")
	botToken, err := client.getBotToken()
	if err != nil {
		fmt.Println("❌ Error get bot token:", err)
		return
	}
	fmt.Println("✅ Bot token didapat")

	fmt.Println("📡 Mengambil graph token...")
	graphToken, err := client.getGraphToken()
	if err != nil {
		fmt.Println("❌ Error get graph token:", err)
		return
	}
	fmt.Println("✅ Graph token didapat")

	fmt.Printf("🔍 Mencari user: %s\n", client.targetEmail)
	userID, err := client.getUserID(graphToken, client.targetEmail)
	if err != nil {
		fmt.Println("❌ Error get user:", err)
		return
	}
	fmt.Printf("✅ User ID: %s\n", userID)

	// fmt.Println("📲 Install bot ke user...")
	// err = client.installBot(graphToken, userID)
	// if err != nil {
	// 	fmt.Println("❌ Error install bot:", err)
	// 	return
	// }
	// fmt.Println("✅ Bot terinstall")

	fmt.Println("💬 Mengambil chat ID...")
	chatID, err := client.getChatID(graphToken, userID)
	if err != nil {
		fmt.Println("❌ Error get chat ID:", err)
		return
	}
	fmt.Printf("✅ Chat ID: %s\n", chatID)

	fmt.Println("📨 Mengirim pesan...")
	err = client.sendMessage(botToken, chatID, message)
	if err != nil {
		fmt.Println("❌ Error send message:", err)
		return
	}
	fmt.Println("✅ Pesan berhasil dikirim!")
}
