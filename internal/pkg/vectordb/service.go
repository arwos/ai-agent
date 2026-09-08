package vectordb

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/qdrant/go-client/qdrant"
)

const searchLimit = 10

type commandFunc func(context.Context, string, ...string) *exec.Cmd

type Service struct {
	Folder         string
	Address        string
	commandContext commandFunc

	clientOnce sync.Once
	client     *qdrant.Client
	clientErr  error
}

func New(folder, address string) *Service {
	return &Service{
		Folder:         folder,
		Address:        address,
		commandContext: exec.CommandContext,
	}
}

// Start launches Qdrant in Docker and returns its process handle.
func (s *Service) Start(ctx context.Context) (*os.Process, error) {
	if strings.TrimSpace(s.Folder) == "" {
		return nil, fmt.Errorf("vectordb folder is required")
	}

	cmd := s.commandContext(ctx, "docker", "run", "-p", "6333:6333", "-p", "6334:6334", "-v", s.Folder+":/qdrant/storage", "qdrant/qdrant")
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Qdrant Docker container: %w", err)
	}
	return cmd.Process, nil
}

// Add inserts vectors into a collection and returns their generated point IDs.
func (s *Service) Add(ctx context.Context, collection string, vectors [][]float32) ([]string, error) {
	if err := validateInput(collection, vectors); err != nil {
		return nil, err
	}

	points := make([]*qdrant.PointStruct, len(vectors))
	ids := make([]string, len(vectors))
	for index, vector := range vectors {
		id, err := newPointID()
		if err != nil {
			return nil, fmt.Errorf("generate point ID: %w", err)
		}
		ids[index] = id
		points[index] = &qdrant.PointStruct{
			Id:      qdrant.NewID(id),
			Vectors: qdrant.NewVectors(vector...),
		}
	}

	wait := true
	client, err := s.qdrant()
	if err != nil {
		return nil, err
	}
	if _, err := client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: collection,
		Points:         points,
		Wait:           &wait,
	}); err != nil {
		return nil, fmt.Errorf("add vectors to collection %q: %w", collection, err)
	}
	return ids, nil
}

// Search finds the closest points for each vector in a collection.
func (s *Service) Search(ctx context.Context, collection string, vectors [][]float32) ([][]*qdrant.ScoredPoint, error) {
	if err := validateInput(collection, vectors); err != nil {
		return nil, err
	}

	queries := make([]*qdrant.QueryPoints, len(vectors))
	for index, vector := range vectors {
		queries[index] = &qdrant.QueryPoints{
			CollectionName: collection,
			Query:          qdrant.NewQuery(vector...),
			Limit:          qdrant.PtrOf(uint64(searchLimit)),
		}
	}

	client, err := s.qdrant()
	if err != nil {
		return nil, err
	}
	results, err := client.QueryBatch(ctx, &qdrant.QueryBatchPoints{
		CollectionName: collection,
		QueryPoints:    queries,
	})
	if err != nil {
		return nil, fmt.Errorf("search collection %q: %w", collection, err)
	}

	points := make([][]*qdrant.ScoredPoint, len(results))
	for index, result := range results {
		points[index] = result.GetResult()
	}
	return points, nil
}

func (s *Service) qdrant() (*qdrant.Client, error) {
	s.clientOnce.Do(func() {
		host, port, err := qdrantAddress(s.Address)
		if err != nil {
			s.clientErr = err
			return
		}
		s.client, s.clientErr = qdrant.NewClient(&qdrant.Config{Host: host, Port: port})
	})
	if s.clientErr != nil {
		return nil, fmt.Errorf("create Qdrant client: %w", s.clientErr)
	}
	return s.client, nil
}

func validateInput(collection string, vectors [][]float32) error {
	if collection == "" {
		return fmt.Errorf("collection name is required")
	}
	if len(vectors) == 0 {
		return fmt.Errorf("at least one vector is required")
	}
	dimensions := len(vectors[0])
	if dimensions == 0 {
		return fmt.Errorf("vectors must not be empty")
	}
	for index, vector := range vectors[1:] {
		if len(vector) != dimensions {
			return fmt.Errorf("vector %d has %d dimensions, expected %d", index+1, len(vector), dimensions)
		}
	}
	return nil
}

func qdrantAddress(address string) (string, int, error) {
	if !strings.Contains(address, "://") {
		address = "//" + address
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return "", 0, fmt.Errorf("invalid Qdrant address %q", address)
	}
	port := 6334
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil {
			return "", 0, fmt.Errorf("invalid Qdrant port in %q: %w", address, err)
		}
	}
	return parsed.Hostname(), port, nil
}

func newPointID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", bytes[:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:]), nil
}
