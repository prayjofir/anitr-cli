#!/usr/bin/expect
spawn ./anitr
expect "Seçim yapın:"
send "1\r"
expect "Aranacak Anime:"
send "Mission: Yozakura Family\r"
expect eof
