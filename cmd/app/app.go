package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"sync"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/randomowo/sync_viewer/internal/app"
)

var websitePath = path.Join("website", "app")
var contentPath = path.Join(websitePath, "content")

var users = sync.Map{}

type ConnWrapper struct {
	conn   *websocket.Conn
	rw     sync.RWMutex
	userID uuid.UUID
}

func (c *ConnWrapper) ReadMessage() (messageType int, p []byte, err error) {
	mt, m, err := c.conn.ReadMessage()
	if err != nil {
		c.Close()
		return 0, nil, err
	}

	return mt, m, nil
}

func (c *ConnWrapper) WriteMessage(messageType int, data []byte) error {
	c.rw.Lock()
	defer c.rw.Unlock()

	err := c.conn.WriteMessage(messageType, data)
	if err != nil {
		c.Close()
		return err
	}

	return nil
}

func (c *ConnWrapper) Close() {
	c.rw.Lock()
	defer c.rw.Unlock()

	_ = c.conn.Close()

	users.Delete(c.userID)
}

const msgDelim = '\x00'
const payloadDelim = '\x01'

type MsgType uint8

func (t MsgType) String() string {
	return strconv.Itoa(int(t))
}

const (
	UserInitMsg MsgType = iota + 1
	SelectorInit
	VideoInit
	Video
)

type VideoState uint8

const (
	Paused VideoState = iota + 1
	Playing
	Seek
)

func (v VideoState) String() string {
	return strconv.Itoa(int(v))
}

type VideoPayload struct {
	Video string
	Seek  float64
	State VideoState
}

func (vp VideoPayload) Payload() []byte {
	var buf bytes.Buffer

	buf.WriteString(vp.State.String())
	buf.WriteByte(payloadDelim)
	buf.WriteString(strconv.FormatFloat(vp.Seek, 'f', -1, 64))
	buf.WriteByte(payloadDelim)
	buf.WriteString(vp.Video)

	return buf.Bytes()
}

func ParseVideoPayload(data []byte) (*VideoPayload, error) {
	parts, err := ParseParts(3, payloadDelim, data)
	if err != nil {
		return nil, err
	}

	var vp VideoPayload

	vpt, err := strconv.Atoi(string(parts[0]))
	if err != nil {
		return nil, err
	}

	switch vpt {
	case int(Paused):
		vp.State = Paused
	case int(Playing):
		vp.State = Playing
	case int(Seek):
		vp.State = Seek
	default:
		return nil, fmt.Errorf("invalid video payload state")
	}

	vpSeek, err := strconv.ParseFloat(string(parts[1]), 64)
	if err != nil {
		return nil, err
	}

	vp.Seek = vpSeek
	vp.Video = string(parts[2])

	return &vp, nil
}

type UserState struct {
	isAdmin bool
	Conn    *ConnWrapper
	VideoPayload
}

type Msg struct {
	UserID uuid.UUID
	Type   MsgType
	Data   []byte
}

func (m *Msg) Payload() []byte {
	var buf bytes.Buffer

	buf.Write([]byte(m.Type.String()))
	buf.WriteByte(msgDelim)
	buf.Write([]byte(m.UserID.String()))
	buf.WriteByte(msgDelim)
	buf.Write(m.Data)

	return buf.Bytes()
}

func ParseParts(partsLen int, delim byte, data []byte) ([][]byte, error) {
	var (
		shift     int
		dataShift int
	)

	parts := make([][]byte, partsLen)

	for i := range data {
		if data[i] == delim {
			if shift >= partsLen {
				return nil, fmt.Errorf("wrong msg, more than %d parts", partsLen)
			}

			parts[shift] = data[dataShift:i]
			dataShift = i + 1

			shift++
		}
	}

	return parts, nil
}

func ParseMsg(data []byte) (*Msg, error) {
	parts, err := ParseParts(3, msgDelim, data)
	if err != nil {
		return nil, err
	}

	var msg Msg

	val, err := strconv.Atoi(string(parts[0]))
	if err != nil {
		return nil, err
	}

	switch val {
	case int(UserInitMsg):
		msg.Type = UserInitMsg
	case int(SelectorInit):
		msg.Type = SelectorInit
	case int(VideoInit):
		msg.Type = VideoInit
	case int(Video):
		msg.Type = Video
	default:
		return nil, fmt.Errorf("invalid msg type, %d", val)
	}

	usedID, err := uuid.Parse(string(parts[1]))
	if err != nil && msg.Type != UserInitMsg {
		return nil, err
	}

	msg.UserID = usedID
	msg.Data = parts[2]

	return &msg, nil
}

func main() {

	server := fiber.New()

	server.Add(
		http.MethodGet, "/ws", websocket.New(
			func(c *websocket.Conn) {
				var err error

				conn := &ConnWrapper{
					conn:   c,
					rw:     sync.RWMutex{},
					userID: uuid.Nil,
				}

				for {
					msgType, msg, err := conn.ReadMessage()

					if err != nil {
						break
					}

					log.Printf("Read: mt %d, %s", msgType, msg)

					switch msgType {
					case websocket.TextMessage:
						err = conn.WriteMessage(websocket.TextMessage, []byte("unsupported message"))
					case websocket.BinaryMessage:
						resp, respCode, err := processBMsg(conn, msg)
						if err == nil && resp != nil {
							err = conn.WriteMessage(respCode, resp.Payload())
						} else if err != nil {
							log.Printf("Error processing BMsg: %s", err)
						}

					case websocket.CloseMessage:
						parsedMsg, err := ParseMsg(msg)
						if err != nil {
							log.Printf("Error parsing message: %s", err)
							continue
						}

						users.Delete(parsedMsg.UserID.String())
					case websocket.PingMessage:
						err = conn.WriteMessage(websocket.PongMessage, nil)
					case websocket.PongMessage:
					}

					if err != nil {
						break
					}
				}

				log.Println("Error:", err)
			},
		),
	)
	server.Static("/", "website/app")

	ctx := context.Background()

	app.Run(ctx, server)
}

func processBMsg(conn *ConnWrapper, data []byte) (*Msg, int, error) {
	msg, err := ParseMsg(data)
	if err != nil {
		return nil, 0, err
	}

	switch msg.Type {
	case UserInitMsg:
		if msg.UserID.String() == uuid.Nil.String() {
			msg.UserID = uuid.New()
		}

		if _, exists := users.Load(msg.UserID.String()); !exists {
			msg.UserID = uuid.New()
			users.Store(
				msg.UserID.String(), UserState{
					Conn: conn,
				},
			)
		}

		conn.userID = msg.UserID

		return &Msg{
			Type:   UserInitMsg,
			UserID: msg.UserID,
			Data:   []byte(msg.UserID.String()),
		}, websocket.BinaryMessage, nil
	case SelectorInit:
		res, pErr := processSelectorInit(msg)
		if pErr != nil {
			return nil, 0, pErr
		}

		return res, websocket.BinaryMessage, nil
	case VideoInit:
		userState, exists := users.Load(msg.UserID.String())
		if !exists {
			panic(fmt.Sprintf("user does not exist: %s", msg.UserID))
		}
		us, ok := userState.(UserState)
		if !ok {
			panic(fmt.Sprintf("user state is not a UserState: %#v", userState))
		}

		us.Seek = 0
		us.Video = string(msg.Data)
		us.State = Paused
		us.Conn = conn

		users.Store(msg.UserID.String(), us)

		go shoutToUsers(&conn.userID, VideoInit, us)

		return nil, 0, nil
	case Video:
		vp, err := ParseVideoPayload(msg.Data)
		if err != nil {
			return nil, 0, err
		}

		userState, exists := users.Load(msg.UserID.String())
		if !exists {
			panic(fmt.Sprintf("user does not exist: %s", msg.UserID))
		}
		us, ok := userState.(UserState)
		if !ok {
			panic(fmt.Sprintf("user state is not a UserState: %#v", userState))
		}

		us.Seek = vp.Seek
		us.Video = vp.Video
		us.State = vp.State
		us.Conn = conn

		users.Store(conn.userID.String(), us)

		go shoutToUsers(&conn.userID, Video, us)

		return nil, 0, nil
	default:
		return nil, 0, fmt.Errorf("invalid msg type, %d", int(msg.Type))
	}
}

func shoutToUsers(from *uuid.UUID, mt MsgType, state UserState) {
	users.Range(
		func(k, v any) bool {
			if from == nil || k.(string) != from.String() {
				us, ok := v.(UserState)
				if !ok {
					panic(fmt.Sprintf("user state is not a UserState: %v", v))
				}

				if us.Seek == state.Seek && us.Video == state.Video && us.State == state.State {
					return true
				}

				users.Store(
					k, UserState{
						isAdmin:      us.isAdmin,
						Conn:         us.Conn,
						VideoPayload: state.VideoPayload,
					},
				)

				var payload []byte
				if mt == VideoInit {
					payload = []byte(state.Video)
				} else if mt == Video {
					payload = state.Payload()
				} else {
					panic(fmt.Sprintf("invalid msg type, %d", int(mt)))
				}

				err := us.Conn.WriteMessage(
					websocket.BinaryMessage, (&Msg{
						Type:   mt,
						UserID: us.Conn.userID,
						Data:   payload,
					}).Payload(),
				)

				if err != nil {
					log.Printf("Error writing message for (%s): %s", k, err)
				}
			}

			return true
		},
	)
}

func processSelectorInit(msg *Msg) (*Msg, error) {
	entries, err := os.ReadDir(contentPath)
	if err != nil {
		return nil, err
	}

	return &Msg{
		Type:   SelectorInit,
		UserID: msg.UserID,
		Data: func() []byte {
			var (
				buf    bytes.Buffer
				length int
			)

			for i := range entries {
				if !entries[i].IsDir() {
					buf.WriteString(entries[i].Name())
					buf.WriteByte(payloadDelim)
					length += len([]byte(entries[i].Name())) + 1
				}
			}

			buf.Truncate(length - 1)

			return buf.Bytes()
		}(),
	}, nil
}
