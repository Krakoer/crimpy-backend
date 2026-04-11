package utils

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type ResendEmailRequest struct {
	From       string                 `json:"from"`
	To         []string               `json:"to"`
	TemplateID string                 `json:"template_id"`
	Variables  map[string]interface{} `json:"variables"`
}

func GenerateVerificationToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func SendVerificationEmail(email, firstname, verificationToken string, isCoach bool) error {
	resendAPIKey := os.Getenv("RESEND_API_KEY")
	emailFrom := os.Getenv("RESEND_EMAIL_FROM")

	if resendAPIKey == "" || emailFrom == "" {
		return fmt.Errorf("RESEND_API_KEY or RESEND_EMAIL_FROM not set")
	}

	verificationLink := fmt.Sprintf("https://api.portfolio-online.ovh/auth/verify?token=%s", verificationToken)

	templateID := "email-verification-normal"
	if isCoach {
		templateID = "email-verification-coach"
	}

	reqBody := ResendEmailRequest{
		From:       emailFrom,
		To:         []string{email},
		TemplateID: templateID,
		Variables: map[string]interface{}{
			"firstname":         firstname,
			"verification_link": verificationLink,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+resendAPIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("resend API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}
