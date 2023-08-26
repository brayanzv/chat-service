package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/rs/cors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Client struct {
	conn *websocket.Conn
	send chan []byte
}

type ChatRoom struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
}

type ChatMessage struct {
	Username  string    `bson:"username"`
	Content   string    `bson:"content"`
	Timestamp time.Time `bson:"timestamp"`
}

func main() {

	chat := &ChatRoom{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte),
	}
	go chat.run()

	router := mux.NewRouter()
	router.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(chat, w, r)
	})

	corsHandler := cors.New(cors.Options{
		AllowedOrigins: []string{"http://localhost:4200"}, // Agrega aquí el origen de tu aplicación Angular
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"*"},
	})

	// Agrega el manejador CORS al router
	router.Use(corsHandler.Handler)

	log.Println("Server started on localhost:8001")
	log.Fatal(http.ListenAndServe(":8001", router))
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// ...

// ... (importaciones y definiciones previas)

// Importar paquetes...

type Message struct {
	Username  string    `json:"username"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// ... Código del servidor ...

func serveWs(chat *ChatRoom, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	client := &Client{
		conn: conn,
		send: make(chan []byte, 256),
	}
	chat.register <- client

	go func() {
		client := connectToMongoDB()
		defer client.Disconnect(context.Background())

		collection := client.Database("chat").Collection("messages")
		options := options.Find()
		options.SetSort(bson.D{{"timestamp", 1}})
		cursor, err := collection.Find(context.Background(), bson.D{}, options)
		if err != nil {
			log.Println("Error al recuperar mensajes:", err)
			return
		}

		for cursor.Next(context.Background()) {
			var chatMessage ChatMessage
			err := cursor.Decode(&chatMessage)
			if err != nil {
				log.Println("Error al decodificar mensaje:", err)
				continue
			}

			message := Message{
				Username:  chatMessage.Username,
				Content:   chatMessage.Content,
				Timestamp: chatMessage.Timestamp,
			}
			messageJSON, err := json.Marshal(message)
			if err != nil {
				log.Println("Error al formatear el mensaje como JSON:", err)
				continue
			}

			err = conn.WriteMessage(websocket.TextMessage, messageJSON)
			if err != nil {
				log.Println(err)
				break
			}
		}
	}()

	go client.writePump()
	go client.readPump(chat)
}

// ... (resto del código del servidor)

// ...

func (chat *ChatRoom) run() {
	for {
		select {
		case client := <-chat.register:
			chat.clients[client] = true
		case client := <-chat.unregister:
			if _, ok := chat.clients[client]; ok {
				delete(chat.clients, client)
				close(client.send)
			}
			// ...

		case message := <-chat.broadcast:
			var msg Message
			err := json.Unmarshal(message, &msg)
			if err != nil {
				log.Println("Error al deserializar el mensaje:", err)
				continue
			}

			for client := range chat.clients {
				select {
				case client.send <- message:
				default:
					delete(chat.clients, client)
					close(client.send)
				}
			}

			// Guardar el mensaje en MongoDB
			go func(msg Message) {
				client := connectToMongoDB()
				defer client.Disconnect(context.Background())

				collection := client.Database("chat").Collection("messages")
				chatMessage := ChatMessage{
					Username:  msg.Username,
					Content:   msg.Content,
					Timestamp: time.Now(),
				}

				_, err := collection.InsertOne(context.Background(), chatMessage)
				if err != nil {
					log.Println("Error al guardar el mensaje en MongoDB:", err)
				}
			}(msg)

			// ...
		}
	}
}

func (c *Client) readPump(chat *ChatRoom) {
	defer func() {
		chat.unregister <- c
		c.conn.Close()
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		chat.broadcast <- message
	}
}

func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.conn.WriteMessage(websocket.TextMessage, message)
		}
	}
}

func connectToMongoDB() *mongo.Client {
	clientOptions := options.Client().ApplyURI("mongodb+srv://devdb:newDev147@cluster0.4frrz48.mongodb.net/")
	client, err := mongo.Connect(context.Background(), clientOptions)
	if err != nil {
		log.Fatal(err)
	}
	return client
}
