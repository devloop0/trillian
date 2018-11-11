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

func NewLeafData (data []byte) (*trillian.LogLeaf) {
	return &trillian.LogLeaf{LeafValue: data}
}

func PrepareLeafData ()publicKey string, deviceId string) ([]byte, error) {
	data, err := json.Marshal (UserTypes.CreateLeafData (newPk, identifier))
	if (err != nil) {
		return nil, err
	}
	return data, nil
}

func GatherLeaves (ctx context.Context, tree *trillian.Tree, reg extension.Registry, key *UserTypes.MapKey, identifier string, newPk string) ([]*trillian.LogLeaf, error){
	if (key.PublicKey == "") {
		data, err := PrepareLeafData (newPk, identifier)
		if err != nil {
			return nil, err
		}
		identity := UserTypes.CreateIdentity(key.UserId, newPk, identifier)
		contents :=  UserTypes.CreateMapContents (key.LogId, key.UserId, newPk, identifier, identity)
		err = reg.LogStorage.AddToUserMap (ctx, tree, contents)
		if err != nil {
			return nil, err
		}
		return NewLeafData (data), nil
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
			data, err := PrepareLeafData (newPk, identifier)
			if err != nil {
				return nil, err
			}
			leaves = append (leaves, NewLeafData (data))
			contents :=  UserTypes.CreateMapContents (key.LogId, key.UserId, newPk, identifier, identity)
			err = reg.LogStorage.AddToUserMap (ctx, tree, contents)
			if err != nil {
				return nil, err
			}
		}
		return leaves, nil
	}
}

func GetKeys (ctx context.Context, tree *trillian.Tree, reg extension.Registry, request *trillian.UserReadLeafRequest) ([]string, error) {
	keys, err := req.LogStorage.GetKeys (ctx, tree, request)
	if err != nil {
		return nil, err
	}
	return keys, nil
}
