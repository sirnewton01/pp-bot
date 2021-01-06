package main

import (
	"fmt"
	"github.com/alecthomas/kong"
	"github.com/maltegrosse/go-modemmanager"
	"github.com/omeid/upower-notify/upower"
	"log"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
	"net/url"
	"strings"
	"sync"
	"time"
)

var CLI struct {
	Homeserver    *url.URL `required help:"Matrix homeserver URL"`
	Username      string   `required help:"Matrix username localpart"`
	Password      string   `required help:"Matrix password"`
	DefaultRoomId string   `required help:"Matrix room where default output can go"`
	Userid        string   `required help:"Matrix userid that may command this bot, others are ignored."`
	Battery       string   `required help:"DBus battery name to monitor for power level"`
}

var client *mautrix.Client
var defaultRoom id.RoomID
var t2r sync.Map
var r2t sync.Map

func main() {
	kong.Parse(&CLI)

	log.Println("Logging into", CLI.Homeserver.String(), "as", CLI.Username)
	var err error
	client, err = mautrix.NewClient(CLI.Homeserver.String(), "", "")
	if err != nil {
		panic(err)
	}

	_, err = client.Login(&mautrix.ReqLogin{
		Type:             "m.login.password",
		Identifier:       mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: CLI.Username},
		Password:         CLI.Password,
		StoreCredentials: true,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
	log.Println("Login successful")

	defaultRoom = id.RoomID(CLI.DefaultRoomId)
	_, err = client.JoinRoomByID(defaultRoom)
	if err != nil {
		log.Fatal(err.Error())
	}

	mmgr, err := modemmanager.NewModemManager()
	if err != nil {
		log.Fatal(err.Error())
	}
	version, err := mmgr.GetVersion()
	if err != nil {
		log.Fatal(err.Error())
	}
	log.Println("ModemManager Version: ", version)
	modems, err := mmgr.GetModems()
	if err != nil {
		log.Fatal(err.Error())
	}
	for _, modem := range modems {
		mloc, err := modem.GetLocation()
		if err != nil {
			log.Fatal(err.Error())
		}
		err = mloc.Setup([]modemmanager.MMModemLocationSource{modemmanager.MmModemLocationSourceGpsNmea, modemmanager.MmModemLocationSource3gppLacCi}, true)
		if err != nil {
			log.Fatal(err.Error())
		}
		go listenToModemPropertiesChanged(modem)
	}

	syncer := client.Syncer.(*mautrix.DefaultSyncer)
	timeBegin := time.Now().Unix() * 1000

	syncer.OnEventType(event.StateTopic, func(source mautrix.EventSource, evt *event.Event) {
		topic := evt.Content.AsTopic()
		log.Printf("Updating topic %s %s\n", evt.RoomID, topic.Topic)

		t2r.Store(topic.Topic, evt.RoomID.String())
		r2t.Store(evt.RoomID.String(), topic.Topic)
	})

	go func() {
		for {
			<-time.After(5 * time.Minute)
			
			log.Printf("Checking battery level\n")
			up, err := upower.New(CLI.Battery)
			if err != nil {
				log.Printf("Error getting battery for %s: %s\n", CLI.Battery, err.Error())
				return
			}
			ud, err := up.Get()
			if err != nil {
				log.Printf("Error getting battery state for %s: %s\n", CLI.Battery, err.Error())
				continue
			}

			if ud.Percentage < 16 {
				log.Printf("Sending low battery warning\n")
				client.SendText(id.RoomID(defaultRoom.String()), "I'm dying. Please plug me in.")
			}
		}
	}()

	syncer.OnEventType(event.StateMember, func(source mautrix.EventSource, evt *event.Event) {
		if evt.Sender.String() != CLI.Userid {
			log.Printf("Skipping event: not from the right user\n")
			return
		}

		log.Printf("StateEvent\n")
		me := evt.Content.AsMember()
		if me.Membership.IsInviteOrJoin() {
			log.Printf("Joining room %s\n", evt.RoomID)
			_, err = client.JoinRoomByID(evt.RoomID)
			if err != nil {
				log.Printf("Error joining room: %s\n", err.Error())
			}
		}
	})

	syncer.OnEventType(event.EventMessage, func(source mautrix.EventSource, evt *event.Event) {
		if evt.Timestamp <= timeBegin || evt.Sender.String() != CLI.Userid {
			log.Printf("Skipping event\n")
			return
		}

		msg := evt.Content.AsMessage().Body
		msgPrefix, ok := r2t.Load(evt.RoomID.String())
		if ok && len(msgPrefix.(string)) != 0 {
			msg = msgPrefix.(string) + " " + msg
		}
		if strings.HasPrefix(msg, "sms ") {
			dest := strings.Split(msg, " ")[1]
			txt := strings.Replace(msg, "sms "+dest+" ", "", 1)
			msging, err := modems[0].GetMessaging()
			if err != nil {
				log.Println(err.Error())
			}
			sms, err := msging.CreateSms(dest, txt)
			if err != nil {
				log.Println(err.Error())
			}
			err = sms.Send()
			if err != nil {
				log.Println(err.Error())
			}
			log.Println("sms " + dest + " " + txt)
			client.SendText(evt.RoomID, "sent")
		} else if strings.HasPrefix(msg, "call ") {
			dest := strings.Split(msg, " ")[1]
			voice, err := modems[0].GetVoice()
			if err != nil {
				log.Println(err.Error())
			}
			_, err = voice.CreateCall(dest)
			if err != nil {
				log.Println(err.Error())
			}
			log.Println("call " + dest)
			client.SendText(evt.RoomID, "calling")
		} else if strings.HasPrefix(msg, "location") {
			mloc, err := modems[0].GetLocation()
			if err != nil {
				log.Println(err.Error())
			}
			loc, err := mloc.GetLocation()
			if err != nil {
				log.Println(err.Error())
			}
			log.Println("location")
			client.SendText(evt.RoomID, fmt.Sprintf("location %s %s\n", loc.GpsNmea, loc.ThreeGppLacCi))
		} else if strings.HasPrefix(msg, "help") {
			log.Println("help")
			client.SendText(evt.RoomID, `help
sms <phone_number> <message>
call <phone_number>
location`)
		} else {
			log.Printf("unknown command %s\n", evt.Content.AsMessage().Body)
			client.SendText(evt.RoomID, "Sorry, I don't understand")
		}
	})

	err = client.Sync()
	if err != nil {
		panic(err)
	}

}
func listenToModemPropertiesChanged(modem modemmanager.Modem) {
	c := modem.SubscribePropertiesChanged()
	for v := range c {
		interfaceName, changedProperties, _, err := modem.ParsePropertiesChanged(v)
		if err != nil {
			log.Println(err.Error())
		} else if interfaceName == "org.freedesktop.ModemManager1.Modem.Messaging" {
			_, ok := changedProperties["Messages"]
			if ok {
				mging, err := modem.GetMessaging()
				if err != nil {
					log.Println(err.Error())
					continue
				}

				msgs, err := mging.List()
				if err != nil {
					log.Println(err.Error())
					continue
				}

				for _, sms := range msgs {
					txt, err := sms.GetText()
					if err != nil {
						log.Println(err.Error())
						continue
					}

					nmbr, err := sms.GetNumber()
					if err != nil {
						log.Println(err.Error())
						continue
					}

					state, err := sms.GetState()
					if err != nil {
						log.Println(err.Error())
						continue
					}
					if state != modemmanager.MmSmsStateReceived {
						continue
					}

					log.Printf("sms %v %v\n", nmbr, txt)
					roomId, ok := t2r.Load("sms " + nmbr)
					m := fmt.Sprintf("%v", txt)
					if !ok {
						roomId = defaultRoom.String()
						m = fmt.Sprintf("sms %v %v", nmbr, txt)
					}
					go func() {
						for {
							_, err := client.SendText(id.RoomID(roomId.(string)), m)
							if err != nil {
								log.Printf("Retrying...\n")
								roomId = defaultRoom.String()
								m = fmt.Sprintf("sms %v %v", nmbr, txt)
								<-time.After(10 * time.Second)
							} else {
								break
							}
						}
					}()

					// TODO delete only after successfully sending the message
					mging.Delete(sms)
				}
			}
		} else if interfaceName == "org.freedesktop.ModemManager1.Modem.Voice" {
			_, ok := changedProperties["Calls"]
			if ok {
				mvoice, err := modem.GetVoice()
				if err != nil {
					log.Println(err.Error())
					continue
				}

				calls, err := mvoice.GetCalls()
				if err != nil {
					log.Println(err.Error())
				}

				for _, call := range calls {
					nmbr, err := call.GetNumber()
					if err != nil {
						log.Println(err.Error())
						continue
					}
					state, err := call.GetState()
					if err != nil {
						log.Println(err.Error())
						continue
					}
					if state != modemmanager.MmCallStateRingingIn {
						continue
					}
					log.Printf("call %v\n", nmbr)

					roomId, ok := t2r.Load("sms " + nmbr)
					m := "<called>"
					if !ok {
						roomId = defaultRoom.String()
						m = fmt.Sprintf("call %v", nmbr)
					}
					client.SendText(id.RoomID(roomId.(string)), m)
				}
			}
		}
	}
}
