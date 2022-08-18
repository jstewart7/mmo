#!/bin/bash

trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT

go run ./server &
sleep 1
go run ./proxy &
sleep 1
cd client
go run . -cpuprofile cpu.prof -memprofile mem.prof
