package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/resend/resend-go/v3"
)

func GenerateVerificationToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func SendVerificationEmail(email, firstname, verificationToken string, isCoach bool) error {
	verificationLink := fmt.Sprintf("%s/verify?token=%s", os.Getenv("BASE_URL"), verificationToken)

	templateID := "email-verification-normal"
	if isCoach {
		templateID = "email-verification-coach"
	}

	return sendTemplateEmail(email, templateID, map[string]interface{}{
		"firstname":         firstname,
		"verification_link": verificationLink,
	})
}

func SendCoachValidatedEmail(email string) error {
	return sendTemplateEmail(email, "account-approved", map[string]interface{}{
		"LOGIN_URL": os.Getenv("BASE_URL"),
	})
}

func SendCoachRejectedEmail(email, firstname string) error {
	return sendTemplateEmail(email, "coach-application-denied", map[string]interface{}{
		"first_name": firstname,
	})
}

func sendTemplateEmail(email, templateID string, variables map[string]interface{}) error {
	resendAPIKey := os.Getenv("RESEND_API_KEY")
	emailFrom := os.Getenv("RESEND_EMAIL_FROM")

	if resendAPIKey == "" || emailFrom == "" {
		if IsTestEnv() {
			return nil
		}
		return fmt.Errorf("RESEND_API_KEY or RESEND_EMAIL_FROM not set")
	}

	client := resend.NewClient(resendAPIKey)

	params := &resend.SendEmailRequest{
		From: emailFrom,
		To:   []string{email},
		Template: &resend.EmailTemplate{
			Id:        templateID,
			Variables: variables,
		},
	}

	_, err := client.Emails.Send(params)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}
