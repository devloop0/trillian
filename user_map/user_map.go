package UserMap

import(
	"context"
	"github.com/google.com/trillian"
)

type MapKey struct {
	LogId int64
	UserId string
	PublicKey string
}

type UserData struct {
	LogId int64
	UserId string
	PublicKey string
	UserIdentifier string
}


/*
	Composes the MapKey from the UserData it seeks to update and the id
	for the tree being used.
*/
func CreateMapKey (data *UserData) *MapKey {
	return &{Logid: data->LogId, UserId: data->UserId, PublicKey: data->PublicKey}
}
