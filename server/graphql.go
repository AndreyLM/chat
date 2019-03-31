//go:generate go run github.com/99designs/gqlgen

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/segmentio/ksuid"

	"github.com/99designs/gqlgen/handler"
	"github.com/go-redis/redis"
	"github.com/gorilla/websocket"
	"github.com/rs/cors"
	"github.com/tinrab/retry"
)

// GraphQLServer - server
type GraphQLServer struct {
	redisClient     *redis.Client
	messageChannels map[string]chan *Message
	userChannels    map[string]chan string
	mutex           sync.Mutex
}

// NewGraphQlServer - new server
func NewGraphQlServer(redisURL string) (*GraphQLServer, error) {
	client := redis.NewClient(&redis.Options{
		Addr: redisURL,
	})

	retry.ForeverSleep(2*time.Second, func(_ int) error {
		_, err := client.Ping().Result()
		return err
	})

	return &GraphQLServer{
		redisClient:     client,
		messageChannels: map[string]chan *Message{},
		userChannels:    map[string]chan string{},
		mutex:           sync.Mutex{},
	}, nil
}

// Serve - serves server
func (s *GraphQLServer) Serve(route string, port int) error {
	config := Config{
		Resolvers: s,
	}
	mux := http.NewServeMux()
	mux.Handle(
		route,
		handler.GraphQL(NewExecutableSchema(config),
			handler.WebsocketUpgrader(websocket.Upgrader{
				CheckOrigin: func(r *http.Request) bool {
					return true
				},
			}),
		),
	)
	mux.Handle("/playground", handler.Playground("GraphQL", route))

	handler := cors.AllowAll().Handler(mux)
	return http.ListenAndServe(fmt.Sprintf(":%d", port), handler)
}

// ----------------------QUERIES------------------------------------

// Messages - post message
func (s *GraphQLServer) Messages(ctx context.Context) ([]Message, error) {
	cmd := s.redisClient.LRange("messages", 0, -1)
	if err := cmd.Err(); err != nil {

		log.Println(err)
		return nil, err
	}
	res, err := cmd.Result()
	if err != nil {
		log.Println(err)
		return nil, err
	}

	messages := []Message{}
	for _, msg := range res {
		var m Message
		err = json.Unmarshal([]byte(msg), &m)
		messages = append(messages, m)
	}

	return messages, nil
}

// Users - get all users
func (s *GraphQLServer) Users(ctx context.Context) ([]string, error) {
	cmd := s.redisClient.SMembers("users")
	if err := cmd.Err(); err != nil {
		log.Println(err)
		return nil, err
	}
	res, err := cmd.Result()
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return res, nil
}

// ----------------------MUTATION------------------------------------

// PostMessage - post message
func (s *GraphQLServer) PostMessage(ctx context.Context, user string, text string) (*Message, error) {
	err := s.createUser(user)
	if err != nil {
		return nil, err
	}
	s.mutex.Lock()
	for _, ch := range s.userChannels {
		ch <- user
	}
	s.mutex.Unlock()
	// Create message
	m := &Message{
		ID:        ksuid.New().String(),
		CreatedAt: time.Now().UTC(),
		Text:      text,
		User:      user,
	}

	msg, _ := json.Marshal(m)
	if err := s.redisClient.LPush("messages", msg).Err(); err != nil {
		log.Println(err)
		return nil, err
	}

	s.mutex.Lock()
	for _, ch := range s.messageChannels {
		ch <- m
	}
	s.mutex.Unlock()
	return m, nil
}

func (s *GraphQLServer) createUser(user string) error {
	if err := s.redisClient.SAdd("users", user).Err(); err != nil {
		return err
	}
	s.mutex.Lock()
	for _, ch := range s.userChannels {
		ch <- user
	}
	s.mutex.Unlock()
	return nil
}

// ----------------------SUBSCRIPTIONS------------------------------------

// MessagePosted - post message
func (s *GraphQLServer) MessagePosted(ctx context.Context, user string) (<-chan *Message, error) {
	err := s.createUser(user)
	if err != nil {
		return nil, err
	}

	messages := make(chan *Message, 1)
	s.mutex.Lock()
	s.messageChannels[user] = messages
	s.mutex.Unlock()

	go func() {
		<-ctx.Done()
		s.mutex.Lock()
		delete(s.messageChannels, user)
		s.mutex.Unlock()
	}()

	return messages, nil
}

// UserJoined - post message
func (s *GraphQLServer) UserJoined(ctx context.Context, user string) (<-chan string, error) {
	err := s.createUser(user)
	if err != nil {
		return nil, err
	}

	users := make(chan string, 1)
	s.mutex.Lock()
	s.userChannels[user] = users
	s.mutex.Unlock()

	go func() {
		<-ctx.Done()
		s.mutex.Lock()
		delete(s.userChannels, user)
		s.mutex.Unlock()
	}()

	return users, nil
}

// -------------------------ROOT RESOLVER interface implementation---------

// Mutation - mutation resolver
func (s *GraphQLServer) Mutation() MutationResolver {
	return s
}

// Query - query
func (s *GraphQLServer) Query() QueryResolver {
	return s
}

// Subscription - subscription
func (s *GraphQLServer) Subscription() SubscriptionResolver {
	return s
}
