package service

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

type smtpMessage struct {
	envelopeFrom string
	envelopeTo   string
	data         []byte
}

func buildSMTPMessage(config *SMTPConfig, to, subject, body string) (smtpMessage, error) {
	if config == nil {
		return smtpMessage{}, errors.New("missing SMTP configuration")
	}

	fromAddress, err := parseSMTPAddress(config.From, "from")
	if err != nil {
		return smtpMessage{}, err
	}
	recipientAddress, err := parseSMTPAddress(to, "recipient")
	if err != nil {
		return smtpMessage{}, err
	}
	messageID, err := generateEmailMessageID(fromAddress.Address, config.Host)
	if err != nil {
		return smtpMessage{}, fmt.Errorf("generate message ID: %w", err)
	}

	fromName := sanitizeEmailHeader(config.FromName)
	if strings.TrimSpace(fromName) == "" {
		fromName = fromAddress.Name
	}
	fromHeader := (&mail.Address{
		Name:    fromName,
		Address: fromAddress.Address,
	}).String()
	toHeader := (&mail.Address{
		Name:    recipientAddress.Name,
		Address: recipientAddress.Address,
	}).String()
	subjectHeader := mime.QEncoding.Encode("UTF-8", sanitizeEmailHeader(subject))

	boundary, err := generateEmailMIMEBoundary()
	if err != nil {
		return smtpMessage{}, fmt.Errorf("generate MIME boundary: %w", err)
	}

	var message bytes.Buffer
	fmt.Fprintf(&message, "From: %s\r\n", fromHeader)
	fmt.Fprintf(&message, "To: %s\r\n", toHeader)
	fmt.Fprintf(&message, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&message, "Message-ID: %s\r\n", messageID)
	fmt.Fprintf(&message, "Subject: %s\r\n", subjectHeader)
	fmt.Fprintf(&message, "MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundary)

	plainText := htmlToPlainText(body)
	if err := writeQuotedPrintableMIMEPart(&message, boundary, "text/plain", plainText); err != nil {
		return smtpMessage{}, err
	}
	if err := writeQuotedPrintableMIMEPart(&message, boundary, "text/html", body); err != nil {
		return smtpMessage{}, err
	}
	fmt.Fprintf(&message, "--%s--\r\n", boundary)

	return smtpMessage{
		envelopeFrom: fromAddress.Address,
		envelopeTo:   recipientAddress.Address,
		data:         message.Bytes(),
	}, nil
}

func generateEmailMIMEBoundary() (string, error) {
	randomID := make([]byte, 18)
	if _, err := rand.Read(randomID); err != nil {
		return "", err
	}
	return "=_sub2api_" + hex.EncodeToString(randomID), nil
}

func writeQuotedPrintableMIMEPart(message *bytes.Buffer, boundary, contentType, content string) error {
	fmt.Fprintf(message, "--%s\r\nContent-Type: %s; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary, contentType)

	writer := quotedprintable.NewWriter(message)
	if _, err := writer.Write([]byte(content)); err != nil {
		return fmt.Errorf("encode %s email part: %w", contentType, err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close %s email part encoder: %w", contentType, err)
	}
	message.WriteString("\r\n")
	return nil
}

func htmlToPlainText(body string) string {
	document, err := xhtml.Parse(strings.NewReader(body))
	if err != nil {
		return strings.TrimSpace(body)
	}

	var text strings.Builder
	appendLineBreak := func() {
		if text.Len() == 0 {
			return
		}
		if !strings.HasSuffix(text.String(), "\n") {
			text.WriteByte('\n')
		}
	}

	var visit func(*xhtml.Node)
	visit = func(node *xhtml.Node) {
		if node == nil {
			return
		}
		if node.Type == xhtml.TextNode {
			text.WriteString(strings.ReplaceAll(node.Data, "\u00a0", " "))
			return
		}
		if node.Type != xhtml.ElementNode && node.Type != xhtml.DocumentNode {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				visit(child)
			}
			return
		}

		tag := strings.ToLower(node.Data)
		switch tag {
		case "head", "script", "style", "title", "noscript":
			return
		case "br":
			appendLineBreak()
			return
		}

		isBlock := isEmailTextBlockElement(tag)
		if isBlock {
			appendLineBreak()
		}
		if tag == "li" {
			text.WriteString("- ")
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
		if tag == "a" {
			for _, attr := range node.Attr {
				if strings.EqualFold(attr.Key, "href") && strings.TrimSpace(attr.Val) != "" {
					text.WriteString(" <")
					text.WriteString(strings.TrimSpace(attr.Val))
					text.WriteString(">")
					break
				}
			}
		}
		if isBlock {
			appendLineBreak()
		}
	}

	visit(document)
	return normalizeEmailPlainText(text.String())
}

func isEmailTextBlockElement(tag string) bool {
	switch tag {
	case "address", "article", "aside", "blockquote", "div", "dl", "dt", "dd", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "li", "main", "ol", "p", "pre", "section", "table", "tbody", "td", "tfoot", "th", "thead", "tr", "ul":
		return true
	default:
		return false
	}
}

func normalizeEmailPlainText(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" && (len(normalized) == 0 || normalized[len(normalized)-1] == "") {
			continue
		}
		normalized = append(normalized, line)
	}
	return strings.TrimSpace(strings.Join(normalized, "\n"))
}

func parseSMTPAddress(value, field string) (*mail.Address, error) {
	if strings.ContainsAny(value, "\r\n") {
		return nil, fmt.Errorf("invalid SMTP %s address: contains a line break", field)
	}

	cleaned := strings.TrimSpace(value)
	address, err := mail.ParseAddress(cleaned)
	if err != nil || strings.TrimSpace(address.Address) == "" {
		if err == nil {
			err = fmt.Errorf("address is empty")
		}
		return nil, fmt.Errorf("invalid SMTP %s address: %w", field, err)
	}
	return address, nil
}

func generateEmailMessageID(fromAddress, smtpHost string) (string, error) {
	randomID := make([]byte, 16)
	if _, err := rand.Read(randomID); err != nil {
		return "", err
	}

	domain := strings.TrimSpace(sanitizeEmailHeader(smtpHost))
	if at := strings.LastIndexByte(fromAddress, '@'); at >= 0 && at < len(fromAddress)-1 {
		domain = fromAddress[at+1:]
	}
	domain = strings.Trim(domain, "[]<>")
	if domain == "" {
		domain = "localhost"
	}

	return fmt.Sprintf("<%s@%s>", hex.EncodeToString(randomID), domain), nil
}
