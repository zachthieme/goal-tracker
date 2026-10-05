// Package directory reads the org's directory, where each person's Manager
// comes from (ADR 0008), and keeps the tool's Accounts in step with it: a sync
// at startup, then hourly, and whenever an Admin asks.
package directory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// Authentik reads people from Authentik's users API. It is a domain.Directory.
type Authentik struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewAuthentik reads the directory of the Authentik at baseURL (e.g.
// http://localhost:9000) with token, an API token that can read users.
func NewAuthentik(baseURL, token string) *Authentik {
	return &Authentik{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// authentikPageSize is how many users People asks Authentik for at a time.
const authentikPageSize = 100

// authentikPage is one page of Authentik's users API, as far as People reads it.
type authentikPage struct {
	Pagination struct {
		// Next is the next page's number, 0 on the last page.
		Next int `json:"next"`
	} `json:"pagination"`
	Results []struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		// Type is internal, external, service_account or
		// internal_service_account.
		Type       string         `json:"type"`
		Attributes map[string]any `json:"attributes"`
	} `json:"results"`
}

// People lists every user in the directory, following the API's pagination,
// with their Name and the email in their manager attribute. Service accounts
// and users without an email aren't people, and are left out. It returns an
// error, and no one, if any page can't be read.
func (a *Authentik) People(ctx context.Context) ([]domain.DirectoryPerson, error) {
	var people []domain.DirectoryPerson
	for page := 1; page != 0; {
		p, err := a.page(ctx, page)
		if err != nil {
			return nil, err
		}
		for _, u := range p.Results {
			if u.Type == "service_account" || u.Type == "internal_service_account" || strings.TrimSpace(u.Email) == "" {
				continue
			}
			manager, _ := u.Attributes["manager"].(string)
			people = append(people, domain.DirectoryPerson{Email: u.Email, Name: u.Name, ManagerEmail: manager})
		}
		if p.Pagination.Next != 0 && p.Pagination.Next <= page {
			return nil, fmt.Errorf("authentik users page %d points back to page %d", page, p.Pagination.Next)
		}
		page = p.Pagination.Next
	}
	return people, nil
}

// page reads one page of the users API.
func (a *Authentik) page(ctx context.Context, page int) (authentikPage, error) {
	q := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(authentikPageSize)}}
	u := a.baseURL + "/api/v3/core/users/?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return authentikPage{}, fmt.Errorf("authentik users: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return authentikPage{}, fmt.Errorf("authentik users: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return authentikPage{}, fmt.Errorf("authentik users page %d: %s: %s", page, resp.Status, strings.TrimSpace(string(body)))
	}
	var p authentikPage
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return authentikPage{}, fmt.Errorf("authentik users page %d: %w", page, err)
	}
	return p, nil
}
