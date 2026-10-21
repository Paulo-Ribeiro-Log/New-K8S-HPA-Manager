package rbac

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Consulta de grupos do Entra ID por Object ID: nome do grupo e seus membros. Mesma técnica do
// resto do pacote (token Graph via `az`, leitura simples). Só leitura.

const (
	groupInfoCacheTTL    = 30 * time.Minute
	groupMembersCacheTTL = 5 * time.Minute
	getByIDsChunk        = 500  // limite do Graph é 1000 ids por chamada
	maxGroupMembers      = 5000 // trava de segurança: grupos enormes não devem estourar a resposta
)

// GroupInfo é um grupo do Entra ID resolvido pelo Object ID.
type GroupInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// GroupMember é um membro (usuário, service principal ou grupo aninhado) de um grupo.
type GroupMember struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Mail        string `json:"mail,omitempty"`
	UPN         string `json:"upn,omitempty"`
	Type        string `json:"type"` // user | group | servicePrincipal | other
}

// GroupMembers é o resultado da leitura dos membros de um grupo.
type GroupMembers struct {
	GroupID   string        `json:"groupId"`
	Members   []GroupMember `json:"members"`
	Truncated bool          `json:"truncated"`
}

// AADGroupDirectory resolve nomes e membros de grupos por Object ID, com cache em memória.
type AADGroupDirectory struct {
	mu      sync.Mutex
	names   map[string]nameEntry
	members map[string]membersEntry
}

type nameEntry struct {
	info      GroupInfo
	found     bool
	expiresAt time.Time
}

type membersEntry struct {
	data      GroupMembers
	expiresAt time.Time
}

func NewAADGroupDirectory() *AADGroupDirectory {
	return &AADGroupDirectory{names: map[string]nameEntry{}, members: map[string]membersEntry{}}
}

// memberTypeFromOData converte "#microsoft.graph.user" em "user".
func memberTypeFromOData(odata string) string {
	switch strings.ToLower(strings.TrimPrefix(odata, "#microsoft.graph.")) {
	case "user":
		return "user"
	case "group":
		return "group"
	case "serviceprincipal":
		return "servicePrincipal"
	default:
		return "other"
	}
}

// ResolveGroupNames devolve o nome de cada Object ID. IDs que não existem como grupo (ou que o
// Graph não devolve) ficam fora do mapa — o chamador mostra só o ID.
func (d *AADGroupDirectory) ResolveGroupNames(ctx context.Context, ids []string) (map[string]GroupInfo, error) {
	out := make(map[string]GroupInfo, len(ids))
	var missing []string
	now := time.Now()

	d.mu.Lock()
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		if e, ok := d.names[id]; ok && now.Before(e.expiresAt) {
			if e.found {
				out[id] = e.info
			}
			continue
		}
		missing = append(missing, id)
	}
	d.mu.Unlock()

	if len(missing) == 0 {
		return out, nil
	}
	token, err := getGraphAccessToken(ctx)
	if err != nil {
		return out, err
	}

	for start := 0; start < len(missing); start += getByIDsChunk {
		end := start + getByIDsChunk
		if end > len(missing) {
			end = len(missing)
		}
		chunk := missing[start:end]
		found, err := graphGetByIDs(ctx, token, chunk)
		if err != nil {
			return out, err
		}
		d.mu.Lock()
		for _, id := range chunk {
			info, ok := found[id]
			d.names[id] = nameEntry{info: info, found: ok, expiresAt: time.Now().Add(groupInfoCacheTTL)}
			if ok {
				out[id] = info
			}
		}
		d.mu.Unlock()
	}
	return out, nil
}

// graphGetByIDs usa POST /directoryObjects/getByIds (leitura, em lote) restrito a grupos.
func graphGetByIDs(ctx context.Context, token string, ids []string) (map[string]GroupInfo, error) {
	payload, _ := json.Marshal(map[string]any{"ids": ids, "types": []string{"group"}})
	ctx, cancel := context.WithTimeout(ctx, graphRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphAPIBaseURL+"/directoryObjects/getByIds?$select=id,displayName", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: graphRequestTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("falha ao chamar Graph API: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("graph API retornou %d: %s", resp.StatusCode, string(body))
	}
	return parseGetByIDs(body)
}

func parseGetByIDs(body []byte) (map[string]GroupInfo, error) {
	var page struct {
		Value []GroupInfo `json:"value"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("falha ao parsear resposta Graph: %w", err)
	}
	out := make(map[string]GroupInfo, len(page.Value))
	for _, g := range page.Value {
		out[strings.ToLower(g.ID)] = g
	}
	return out, nil
}

// GroupMembers lista os membros diretos do grupo (`transitive` inclui os de grupos aninhados).
func (d *AADGroupDirectory) GroupMembers(ctx context.Context, groupID string, transitive bool) (*GroupMembers, error) {
	groupID = strings.ToLower(strings.TrimSpace(groupID))
	key := fmt.Sprintf("%s|%t", groupID, transitive)

	d.mu.Lock()
	if e, ok := d.members[key]; ok && time.Now().Before(e.expiresAt) {
		d.mu.Unlock()
		data := e.data
		return &data, nil
	}
	d.mu.Unlock()

	token, err := getGraphAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	edge := "members"
	if transitive {
		edge = "transitiveMembers"
	}
	nextURL := fmt.Sprintf("%s/groups/%s/%s?$select=id,displayName,mail,userPrincipalName&$top=999",
		graphAPIBaseURL, url.PathEscape(groupID), edge)

	result := GroupMembers{GroupID: groupID, Members: []GroupMember{}}
	for nextURL != "" {
		var page struct {
			Value []struct {
				ODataType         string `json:"@odata.type"`
				ID                string `json:"id"`
				DisplayName       string `json:"displayName"`
				Mail              string `json:"mail"`
				UserPrincipalName string `json:"userPrincipalName"`
			} `json:"value"`
			NextLink string `json:"@odata.nextLink"`
		}
		if _, err := graphGet(ctx, token, nextURL, nil, &page); err != nil {
			return nil, err
		}
		for _, m := range page.Value {
			if len(result.Members) >= maxGroupMembers {
				result.Truncated = true
				break
			}
			result.Members = append(result.Members, GroupMember{
				ID: m.ID, DisplayName: m.DisplayName, Mail: m.Mail, UPN: m.UserPrincipalName,
				Type: memberTypeFromOData(m.ODataType),
			})
		}
		if result.Truncated {
			break
		}
		nextURL = page.NextLink
	}
	sortMembers(result.Members)

	d.mu.Lock()
	d.members[key] = membersEntry{data: result, expiresAt: time.Now().Add(groupMembersCacheTTL)}
	d.mu.Unlock()
	return &result, nil
}

// sortMembers ordena por tipo (grupos aninhados primeiro) e nome, sem diferenciar maiúsculas.
func sortMembers(ms []GroupMember) {
	rank := map[string]int{"group": 0, "user": 1, "servicePrincipal": 2, "other": 3}
	sort.SliceStable(ms, func(i, j int) bool {
		if ri, rj := rank[ms[i].Type], rank[ms[j].Type]; ri != rj {
			return ri < rj
		}
		return strings.ToLower(ms[i].DisplayName) < strings.ToLower(ms[j].DisplayName)
	})
}
