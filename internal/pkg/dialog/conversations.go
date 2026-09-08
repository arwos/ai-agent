package dialog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

const conversationMetaFile = "conversation.json"

var ErrConversationNotFound = errors.New("conversation not found")

func (s *Store) conversationPath(profileID, parentID, id string) string {
	if parentID != "" {
		return filepath.Join(s.Root, profileID, parentID, "subconversation", id, conversationMetaFile)
	}
	return filepath.Join(s.Root, profileID, id, conversationMetaFile)
}

func (s *Store) SaveConversation(item models.Conversation) (models.Conversation, error) {
	if !safePart(item.ProfileID) || !safePart(item.ID) || (item.ParentID != "" && !safePart(item.ParentID)) {
		return item, errors.New("invalid conversation identifier")
	}
	if item.Title == "" {
		item.Title = "New conversation"
	}
	item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	path := s.conversationPath(item.ProfileID, item.ParentID, item.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return item, err
	}
	b, err := json.Marshal(item)
	if err != nil {
		return item, err
	}
	return item, os.WriteFile(path, append(b, '\n'), 0644)
}

func (s *Store) Conversation(profileID, id string) (models.Conversation, error) {
	items, err := s.ProfileConversations(profileID)
	if err != nil {
		return models.Conversation{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return models.Conversation{}, ErrConversationNotFound
}

func (s *Store) ProfileConversations(profileID string) ([]models.Conversation, error) {
	out := []models.Conversation{}
	err := filepath.WalkDir(filepath.Join(s.Root, profileID), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != conversationMetaFile {
			return err
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var item models.Conversation
		if json.Unmarshal(b, &item) == nil && item.ProfileID == profileID && item.ID != "" {
			out = append(out, item)
		}
		return nil
	})
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

func (s *Store) Conversations(profileID, workspaceID string) ([]models.Conversation, error) {
	items, err := s.ProfileConversations(profileID)
	if err != nil {
		return nil, err
	}
	out := items[:0]
	for _, item := range items {
		if item.WorkspaceID == workspaceID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (s *Store) DeleteConversation(item models.Conversation) error {
	return os.Remove(s.conversationPath(item.ProfileID, item.ParentID, item.ID))
}
