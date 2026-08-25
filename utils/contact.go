package utils

import (
	"errors"
	"net/mail"
	"strings"
)

var ErrInvalidContact = errors.New("invalid contact")

func NormalizeEmail(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized || !strings.Contains(normalized, "@") {
		return "", ErrInvalidContact
	}
	return normalized, nil
}

func NormalizeTZPhone(value string) (string, error) {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for index, character := range value {
		switch {
		case character >= '0' && character <= '9':
			builder.WriteRune(character)
		case character == '+' && index == 0:
		case character == ' ' || character == '-' || character == '(' || character == ')':
		default:
			return "", ErrInvalidContact
		}
	}
	digits := builder.String()

	switch {
	case strings.HasPrefix(digits, "255") && len(digits) == 12:
		digits = digits[3:]
	case strings.HasPrefix(digits, "0") && len(digits) == 10:
		digits = digits[1:]
	case len(digits) == 9:
	default:
		return "", ErrInvalidContact
	}

	if len(digits) != 9 || (digits[0] != '6' && digits[0] != '7') {
		return "", ErrInvalidContact
	}
	return "+255" + digits, nil
}
