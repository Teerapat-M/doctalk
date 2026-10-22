package main

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

var uuidRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validUUID(s string) bool {
	return uuidRE.MatchString(s)
}

func requireJSONBody(c *gin.Context, dst any) bool {
	ct := c.ContentType()
	if ct != "application/json" {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "Content-Type must be application/json"})
		return false
	}
	dec := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return false
	}
	if dec.More() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return false
	}
	return true
}

func invalidDocumentIDs(ids []string) bool {
	for _, id := range ids {
		if !validUUID(id) {
			return true
		}
	}
	return false
}

func parseOptionalK(k *int, def, max int) (int, string) {
	if max < 1 {
		max = defaultAskTopKMax
	}
	if def < 1 {
		def = defaultAskTopK
	}
	if def > max {
		def = max
	}
	if k == nil {
		return def, ""
	}
	if *k < 1 || *k > max {
		return 0, "k is out of range"
	}
	return *k, ""
}

func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && p != "none" {
			out = append(out, p)
		}
	}
	return out
}
