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

const (

	searchMap = "SELECT TreeId, UserId, PublicKey, Identifiers FROM PublicKeyMaps WHERE TreeId=? AND UserId=? AND PublicKey=?"
	addMap = "INSERT INTO PublicKeyMaps(?,?,?,?,?) VALUES"
	deleteMap "DELETE FROM PublicKeyMaps WHERE TreeId=? AND UserId=? AND PublicKey=?"
)

/*
	Composes the MapKey from the UserData it seeks to update and the id
	for the tree being used.
*/
func CreateMapKey (data *UserData) *MapKey {
	return &{Logid: data->LogId, UserId: data->UserId, PublicKey: data->PublicKey}
}

/*
	Makes a SQL query to the database to acquire all nodes that will need
	to be updated for the user.
*/
func searchMap (ctx context.Context, client *trillian.TrillianLogClient, key *MapKey) []*UserData, error {
}

/*
	Deletes all entries with the associated Key from the map.
*/
func deleteFromMap (ctx context.Context, client *trillian.TrillianLogClient, key *Mapkey) error {

}

/*
	Adds new entries to the map for each UserData value and the
	newPk.
*/
func addToMap (ctx context.Context, client *trillian.TrillianLogClient, nodes []*UserData, newPk string) error {

}

/*	Takes in a pointer to UserData that needs to be updated and finds
	all other nodes that need to be updated. Returns a list of all 
	UserData nodes that will need to be updated and errors if no map
	exists.
*/
func GatherNodes (ctx context.Context, client *trillian.TrillianLogClient, datum *UserData) []*UserData, error {
	mapKey := CreateMapKey (datum)
	return searchMap (ctx, client, mapkey)
}

/*
	Function to update the Map in the database. It first adds the new nodes
	and then deletes the previous ones.
*/
func updateMap (ctx context.Context, client *trillian.TrillianLogClient, nodes []*UserData, newPk string) error {
	err := addToMap (ctx, client, nodes, newPk)
	if err != nil {
		return err
	}
	mapKey := CreateMapKey (nodes[0])
	err = deleteFromMap (ctx, client, mapKey)
	return err
}
