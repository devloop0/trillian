type MapKey struct {
	TreeId int64
	UserId string
	PublicKey string
}

type UserData struct {
	UserId string
	PublicKey string
	UserIdentifier string
}

type UserLeafData struct {
	PublicKey string
	UserIdentifier string
}

func CreateMapKey () {

}

/*	Takes in a pointer to UserData that needs to be updated and finds
	all other nodes that need to be updated. Returns a list of all 
	UserData nodes that will need to be updated and errors if no map
	exists.
*/
func GatherNodes (datum *UserData) []*UserData, error  {
}
