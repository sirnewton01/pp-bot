package main

import (
	"fmt"
	"github.com/maltegrosse/go-modemmanager"
	"github.com/alecthomas/kong"
	"log"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
	"strings"
	"time"
	"net/url"
)

var CLI struct {
	Homeserver *url.URL `required help:"Matrix homeserver URL"`
	Username   string   `required help:"Matrix username localpart"`
	Password   string   `required help:"Matrix password"`
	Roomid     string   `required help:"Matrix room to receive commands and send responses"`
}


var room id.RoomID
var client *mautrix.Client

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

	room = id.RoomID(CLI.Roomid)
	_, err = client.JoinRoomByID(room)
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
	syncer.OnEventType(event.EventMessage, func(source mautrix.EventSource, evt *event.Event) {
		if evt.Timestamp <= timeBegin || evt.Sender == client.UserID {
			return
		}
		msg := evt.Content.AsMessage().Body
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
			client.SendText(room, "sent")
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
			client.SendText(room, "calling")
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
			client.SendText(room, fmt.Sprintf("location %s %s\n", loc.GpsNmea, loc.ThreeGppLacCi))
		} else if strings.HasPrefix(msg, "help") {
			log.Println("help")
			client.SendText(room, `help
sms <phone_number> <message>
call <phone_number>
location`)
		} else {
			log.Printf("unknown command %s\n", evt.Content.AsMessage().Body)
			client.SendText(room, "Sorry, I don't understand")
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
					go func() {
						for {
							_, err := client.SendText(room, fmt.Sprintf("sms %v %v", nmbr, txt))
							if err != nil {
								log.Printf("Retrying...\n")
								<-time.After(10 * time.Second)
							} else {
								break
							}
						}
					}()

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
					client.SendText(room, fmt.Sprintf("call %v\n", nmbr))
				}
			}
		}
	}
}
