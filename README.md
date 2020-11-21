PP-Bot
======

Interact with your PinePhone or Linux phone in Matrix IM from any of your
other devices, in particular laptop or desktop.

* Send/receive SMS messages
* Notify on incoming calls
* Initiate an outgoing call (copy/paste from 
* Retrieve phone's current location

Here are some example interactions:

```
bot: sms 5555555555 Some message....
me: sms 5555555555 A reply to the message...
bot: sent
bot: call +11234567890
me: location
bot: location NmeaSentences: [$GPGSA,A,1,,,,,,,,,,,,,,,,*32 $GPRMC,,V,,,,,,,,,,N*53 $GPGSV,1,1,01,40,,,34,1*66 $GPVTG,,T,,M,,N,,K,N*2C $GPGGA,,,,,,0,,,,,,,,*66] Mcc: 302, Mnc: 610, Lac: FFFE, Ci: 1D11F15, Tac: 2D8A
```

PP-Bot uses ModemManager and will only work on Linux installations where it is installed and active.

## Installation and usage

* Install Go 1.14 or later
* Run ```go install```

Note that chatty can delete SMS messages from the phone before PP-Bot can read them. Disabling chatty daemon is recommended.

Trivial launch:
```
pp-bot ... > pp-bot-log.txt
```

Launch on startup with Mobian:
* Create a launcher script (.desktop file)
* Place launcher in /etc/xdg/autostart so that it gets started automatically on initial login


## Future ideas

* Alert
  * Instruct the phone to make repeated noise and sounds so that it is discoverable if lost
  * Show a message on the screen
* Query battery level
* Remote erase of all user data
* Local access to the bot
  * eg. a CLI to pull logs and send commands for testing or lack of internet connection
