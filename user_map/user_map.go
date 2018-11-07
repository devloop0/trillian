package UserMap

import(
	"context"
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
		contents :=  UserTypes.CreateMapContents (key.LogId, key.UserId, newPk, identifier)
		err = reg.LogStorage.AddToUserMap (ctx, tree, contents)
		if err != nil {
			return nil, err
		}
		return []*trillian.LogLeaf{newLeafData (data)}, nil
	} else {
		ids, err := reg.LogStorage.SearchUserMap (ctx, tree, key)
		if (err != nil) {
			return nil, err
		}
		err = reg.LogStorage.DeleteFromUserMap (ctx, tree, key)
		if (err != nil) {
			return nil, err
		}
		leaves := make([]*trillian.LogLeaf, 0)
		for _, id := range ids {
			data, err := json.Marshal (UserTypes.CreateLeafData (newPk, id))
			if err != nil {
				return nil, err
			}
			leaves = append (leaves, newLeafData (data))
			contents :=  UserTypes.CreateMapContents (key.LogId, key.UserId, newPk, id)
			err = reg.LogStorage.AddToUserMap (ctx, tree, contents)
			if err != nil {
				return nil, err
			}
		}
		return leaves, nil
	}
}
