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
	DeviceId string
	Identity string
}

type UserData struct {
	UserId string
	OldPublicKey string
	DeviceId string
	NewPublicKey string
}

type LeafData struct {
	PublicKey string
	DeviceId string
}

/*
        Composes the MapKey from the UserData it seeks to update and the id
        for the tree being used.
*/
func CreateMapKey (LogId int64, UserId string, OldPublicKey string) *MapKey {
        key := MapKey{LogId: LogId, UserId: UserId, PublicKey: OldPublicKey}
        return &key
}

func CreateMapContents (LogId int64, UserId string, PublicKey string, DeviceId string, Identity string) *MapContents {
	return &MapContents{LogId: LogId, UserId: UserId, PublicKey: PublicKey, DeviceId: DeviceId, Identity: Identity}
}

func CreateLeafData (pk string, deviceId string) LeafData  {
        return LeafData{PublicKey: pk, DeviceId: deviceId}
}
