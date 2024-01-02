package main

import (
	"bytes"
	"database/sql"
	"encoding/gob"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/storage/sqlite3"
	"github.com/gofiber/template/html/v2"
)

type user struct {
	Email     string `json:"email"`
	Firstname string `json:"firstname"`
	Lastname  string `json:"lastname"`
}

var users = map[string]user{}

func main() {
	users["jj"] = user{Email: "john.joe@example.com", Firstname: "John", Lastname: "Joe"}
	users["mm"] = user{Email: "mary.moe@example.com", Firstname: "Mary", Lastname: "Moe"}
	users["dd"] = user{Email: "dale.doe@example.com", Firstname: "Dale", Lastname: "Doe"}

	db, err := sql.Open("sqlite3", "./fiber.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	query := `CREATE TABLE IF NOT EXISTS sessions (
		k  VARCHAR(64) PRIMARY KEY NOT NULL DEFAULT '',
		v  BLOB NOT NULL,
		e  BIGINT NOT NULL DEFAULT '0',
		u  TEXT);`

	_, err = db.Exec(query)
	if err != nil {
		log.Fatal(err)
	}

	storage := sqlite3.New(sqlite3.Config{
		Database:        "./fiber.db",
		Table:           "sessions",
		Reset:           false,
		GCInterval:      10 * time.Second,
		MaxOpenConns:    100,
		MaxIdleConns:    100,
		ConnMaxLifetime: 1 * time.Second,
	})

	store := session.New(session.Config{
		Storage:    storage,
		Expiration: 5 * time.Minute,
		KeyLookup:  "cookie:myapp_session",
	})

	engine := html.New("./views", ".html")

	app := fiber.New(fiber.Config{
		Views: engine,
	})

	app.Get("/", func(c *fiber.Ctx) error {
		return c.Render("index", fiber.Map{})
	})

	app.Post("/api/login", func(c *fiber.Ctx) error {
		req := struct {
			UID string `json:"uid"`
		}{}
		if err := c.BodyParser(&req); err != nil {
			log.Println(err)
		}

		s, _ := store.Get(c)

		if s.Fresh() {
			sid := s.ID()
			uid := req.UID

			s.Set("uid", uid)
			s.Set("sid", sid)
			s.Set("ip", c.Context().RemoteIP().String())
			s.Set("login", time.Unix(time.Now().Unix(), 0).UTC().String())
			s.Set("ua", string(c.Request().Header.UserAgent()))

			err := s.Save()
			if err != nil {
				log.Println(err)
			}

			stmt, err := db.Prepare(`UPDATE sessions set u = ? WHERE k = ?`)
			if err != nil {
				log.Println(err)
			}

			_, err = stmt.Exec(uid, sid)
			if err != nil {
				log.Println(err)
			}
		}

		return c.JSON(nil)
	})

	app.Post("/api/logout", func(c *fiber.Ctx) error {
		req := struct {
			SID string `json:"sid"`
		}{}
		if err := c.BodyParser(&req); err != nil {
			log.Println(err)
		}

		s, _ := store.Get(c)

		if len(req.SID) > 0 {
			data, err := store.Storage.Get(req.SID)
			if err != nil {
				log.Println(err)
			}

			gd := gob.NewDecoder(bytes.NewBuffer(data))
			dm := make(map[string]interface{})
			if err := gd.Decode(&dm); err != nil {
				log.Println(err)
			}

			if s.Get("uid").(string) == dm["uid"] {
				store.Storage.Delete(req.SID)
			}
		} else {
			s.Destroy()
		}

		return c.JSON(nil)
	})

	app.Get("/api/account", func(c *fiber.Ctx) error {
		s, _ := store.Get(c)

		if len(s.Keys()) > 0 {
			type session struct {
				SID    string `json:"sid"`
				IP     string `json:"ip"`
				Login  string `json:"login"`
				Expiry string `json:"expiry"`
				UA     string `json:"ua"`
			}
			type account struct {
				Email     string    `json:"email"`
				Firstname string    `json:"firstname"`
				Lastname  string    `json:"lastname"`
				Session   string    `json:"session"`
				Sessions  []session `json:"sessions"`
			}

			sid := s.ID()
			uid := s.Get("uid").(string)
			u := account{
				Email:     users[uid].Email,
				Firstname: users[uid].Firstname,
				Lastname:  users[uid].Lastname,
				Session:   sid,
			}

			rows, err := db.Query(`SELECT v, e FROM sessions WHERE u = ?`, uid)
			if err != nil {
				log.Println(err)
			}

			defer rows.Close()

			for rows.Next() {
				var (
					data       = []byte{}
					exp  int64 = 0
				)
				if err := rows.Scan(&data, &exp); err != nil {
					log.Println(err)
				}

				if exp > time.Now().Unix() {
					gd := gob.NewDecoder(bytes.NewBuffer(data))
					dm := make(map[string]interface{})
					if err := gd.Decode(&dm); err != nil {
						log.Println(err)
					}

					u.Sessions = append(u.Sessions, session{
						SID:    dm["sid"].(string),
						IP:     dm["ip"].(string),
						Login:  dm["login"].(string),
						Expiry: time.Unix(exp, 0).UTC().String(),
						UA:     dm["ua"].(string),
					})
				}
			}
			return c.JSON(u)
		}

		return c.JSON(nil)
	})

	app.Use(func(c *fiber.Ctx) error {
		return c.SendStatus(404)
	})

	log.Fatal(app.Listen(":3000"))
}
