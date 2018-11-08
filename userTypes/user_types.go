package UserTypes

// NICK
type MapKey struct {
	LogId int64
	UserId string
	PublicKey string
}

type MapContents struct {
	LogId int64
	UserId string
	PublicKey string
	UserIdentifier string
}

type UserData struct {
	UserId string
	OldPublicKey string
	UserIdentifier string
	NewPublicKey string
}

type LeafData struct {
	PublicKey string
	UserIdentifier string
}

/*
        Composes the MapKey from the UserData it seeks to update and the id
        for the tree being used.
*/
func CreateMapKey (data *UserData, LogId int64) *MapKey {
        key := MapKey{LogId: LogId, UserId: data.UserId, PublicKey: data.OldPublicKey}
        return &key
}

func CreateMapContents (LogId int64, UserId string, PublicKey string, UserIdentifier string) *MapContents {
        return &MapContents{LogId: LogId, UserId: UserId, PublicKey: PublicKey, UserIdentifier: UserIdentifier}
}

func CreateLeafData (pk string, identifier string) LeafData  {
        return LeafData{PublicKey: pk, UserIdentifier: identifier}
}
