package UserMap

import(
	"context"
	"github.com/golang/glog"
	"github.com/google/trillian"
	"github.com/google/trillian/userTypes"
	"github.com/google/trillian/extension"
	"encoding/json"
)


/*
	Takes in a logLeaf and extracts the necessary data to get a key to the
	map and the new public key.
*/
func ExtractMapKey (q *trillian.QueueLeafRequest) (*UserTypes.MapKey, string, string, error) {
	data := &UserTypes.UserData{}
	err := json.Unmarshal (q.Leaf.LeafValue, data)
	if (err != nil) {
		return nil, "", "", err
	}
	return UserTypes.CreateMapKey(data, q.LogId), data.UserIdentifier, data.NewPublicKey, nil
}

func newLeafData (data []byte) *trillian.LogLeaf {
	return &trillian.LogLeaf{LeafValue: data}
}

func GatherLeaves (ctx context.Context, tree *trillian.Tree, reg extension.Registry, key *UserTypes.MapKey, identifier string, newPk string) ([]*trillian.LogLeaf, error){
	if (key.PublicKey == "") {
		data, err := json.Marshal (UserTypes.CreateLeafData (newPk, identifier))
		if (err != nil) {
			return nil, err
		}
		identity := UserTypes.CreateIdentity(key.UserId, newPk, identifier)
		contents :=  UserTypes.CreateMapContents (key.LogId, key.UserId, newPk, identifier, identity)
		//glog.Errorf("New %s %s %s %s\n", key.UserId, newPk, identifier, identity)
		err = reg.LogStorage.AddToUserMap (ctx, tree, contents)
		if err != nil {
			return nil, err
		}
		glog.Errorf("\n\n")
		return []*trillian.LogLeaf{newLeafData (data)}, nil
	} else {
		identifiers, identities, err := reg.LogStorage.SearchUserMap (ctx, tree, key)
		if (err != nil) {
			return nil, err
		}
		if len(identifiers) != len(identities) {
			panic("Invalid mapping between identifiers and identities.")
		}
		err = reg.LogStorage.DeleteFromUserMap (ctx, tree, key)
		if (err != nil) {
			return nil, err
		}
		leaves := make([]*trillian.LogLeaf, 0)
		for i, _ := range identifiers {
			identifier, identity := identifiers[i], identities[i]
			data, err := json.Marshal (UserTypes.CreateLeafData (newPk, identifier))
			if err != nil {
				return nil, err
			}
			leaves = append (leaves, newLeafData (data))
			contents :=  UserTypes.CreateMapContents (key.LogId, key.UserId, newPk, identifier, identity)
			//glog.Errorf("Updated %s %s %s %s\n", key.UserId, newPk, identifier, identity)
			err = reg.LogStorage.AddToUserMap (ctx, tree, contents)
			if err != nil {
				return nil, err
			}
		}
		glog.Errorf("\n\n")
		return leaves, nil
	}
}
