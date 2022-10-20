package serdes

import (
	"fmt"
	"testing"

	"github.com/unitoftime/flow/physics"
	"github.com/unitoftime/ecs"
)

func TestBinaryEncoding(t *testing.T) {
	// msg := Message{
	// 	Type: ClientLoginType,
	// 	Data: ClientLogin{0xAEAE},
	// }
	{
		dat, err := MarshalBinary(ClientLogin{0xAEAE})
		if err != nil { panic(err) }

		fmt.Printf("ClientLogin: %x\n", dat)

		v, err := UnmarshalBinary(dat)
		if err != nil { panic(err) }
		fmt.Printf("%T: %x\n", v, v)
	}
	{
		dat, err := MarshalBinary(ClientLoginResp{0xAEAE, ecs.Id(0xAAAA)})
		if err != nil { panic(err) }

		fmt.Printf("%x\n", dat)

		v, err := UnmarshalBinary(dat)
		if err != nil { panic(err) }
		fmt.Printf("%T: %x\n", v, v)
	}

	{
		dat, err := MarshalBinary(ClientLogout{0xAEAE})
		if err != nil { panic(err) }

		fmt.Printf("%x\n", dat)

		v, err := UnmarshalBinary(dat)
		if err != nil { panic(err) }
		fmt.Printf("%T: %x\n", v, v)
	}
	{
		dat, err := MarshalBinary(ClientLogoutResp{0xAEAE, ecs.Id(0xAAAA)})
		if err != nil { panic(err) }

		fmt.Printf("%x\n", dat)

		v, err := UnmarshalBinary(dat)
		if err != nil { panic(err) }
		fmt.Printf("%T: %x\n", v, v)
	}

	// World update
	{
		// TODO - Seems like the binary package i'm using doesn't work if I don't pass a pointer here. (because I have a pointer receiver on MarshalBinary()
		dat, err := MarshalBinary(&WorldUpdate{
			UserId: 0xAEAEAE,
			WorldData: map[ecs.Id][]ecs.Component{
				1: []ecs.Component{ecs.C(physics.Transform{1,2,3}), ecs.C(physics.Input{})},
				2: []ecs.Component{ecs.C(physics.Transform{4,5,6})},
				3: []ecs.Component{ecs.C(physics.Input{true,true,true,true})},
			},
			Delete: []ecs.Id{1,2,3,4,5},
			Messages: []ChatMessage{
				ChatMessage{"user", "message"},
				ChatMessage{"user2", "message2"},
			},
		})
		if err != nil { panic(err) }
		// 005b5aaeddba050301000000020018000000000000f03f00000000000000400000000000000840010400000000020000000100180000000000001040000000000000144000000000000018400300000001010401010101050102030405
		// 005b5aaeddba0503030000000101040101010101000000020018000000000000f03f0000000000000040000000000000084001040000000002000000010018000000000000104000000000000014400000000000001840050102030405
		fmt.Printf("Message: %x\n", []byte("test"))
		fmt.Printf("Dat: %x\n", dat)

		v, err := UnmarshalBinary(dat)
		if err != nil { panic(err) }
		fmt.Printf("%T: %v\n", v, v)
	}
}
