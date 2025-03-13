package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

const (
	verifyToken = "YOUR_VERIFY_TOKEN" // for webhook verification
)

func main() {
	http.HandleFunc("/webhook", handleWebhook)
	log.Println("🌐 Webhook server is running on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		verifyWebhook(w, r)
		return
	}

	if r.Method == http.MethodPost {
		handleIncomingMessage(w, r)
		return
	}

	w.WriteHeader(http.StatusMethodNotAllowed)
}

func verifyWebhook(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	if mode == "subscribe" && token == verifyToken {
		fmt.Fprintf(w, "%s", challenge)
		log.Println("✅ Webhook verified successfully!")
		return
	}

	w.WriteHeader(http.StatusForbidden)
}


func handleIncomingMessage(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("❌ Error decoding webhook payload: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	log.Printf("📨 Incoming Message Webhook: %+v\n", payload)

	// Extract sender ID and message text (simplified)
	entry := payload["entry"].([]interface{})[0].(map[string]interface{})
	changes := entry["changes"].([]interface{})[0].(map[string]interface{})
	value := changes["value"].(map[string]interface{})
	messages := value["messages"].([]interface{})

	if len(messages) > 0 {
		msg := messages[0].(map[string]interface{})
		senderID := msg["from"].(string)

		log.Printf("🔔 New message from: %s", senderID)

		// Send a reply
		err := sendReply(senderID, "👋 Hello! Thanks for messaging us.")
		if err != nil {
			log.Printf("❌ Failed to send reply: %v", err)
		}
	}

	w.WriteHeader(http.StatusOK)
}

const (
	pageAccessToken = "YOUR_PAGE_ACCESS_TOKEN"
)

func sendReply(recipientID, messageText string) error {
	url := fmt.Sprintf("https://graph.facebook.com/v18.0/me/messages?access_token=%s", pageAccessToken)

	messageData := map[string]interface{}{
		"recipient": map[string]interface{}{
			"id": recipientID,
		},
		"message": map[string]interface{}{
			"text": messageText,
		},
	}

	body, err := json.Marshal(messageData)
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send message, status: %s", resp.Status)
	}

	return nil
}
