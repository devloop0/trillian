package UserMap

import(
	"context"
	"errors"
	"github.com/google/trillian"
	"github.com/google/trillian/storage"
	"github.com/google/trillian/userTypes"
	"github.com/google/trillian/extension"
	"encoding/json"
)


/*
	Takes in a logLeaf and extracts the necessary data to get a key to the
	map and the new public key.
*/
func ExtractMapKey (LogId int64, UserId string, OldPublicKey string) *UserTypes.MapKey {
	return UserTypes.CreateMapKey(LogId, UserId, OldPublicKey)
}

func NewLeafData (data []byte) (*trillian.LogLeaf) {
	return &trillian.LogLeaf{LeafValue: data}
}

func PrepareLeafData (publicKey string, deviceId string) ([]byte, error) {
	data, err := json.Marshal (UserTypes.CreateLeafData (publicKey, deviceId))
	if (err != nil) {
		return nil, err
	}
	return data, nil
}

func GatherLeaves (ctx context.Context, tree *trillian.Tree, reg extension.Registry, key *UserTypes.MapKey, deviceId string, newPk string) ([]*trillian.LogLeaf, storage.LogTreeTX, error){
	if (key.PublicKey == "") {
		data, err := PrepareLeafData (newPk, deviceId)
		if err != nil {
			return nil, nil, err
		}
		identity := UserTypes.CreateIdentity(key.UserId, newPk, deviceId)
		contents :=  UserTypes.CreateMapContents (key.LogId, key.UserId, newPk, deviceId, identity)
		tx, err := reg.LogStorage.AddToUserMap (ctx, tree, contents)
		if err != nil {
			return nil, nil, err
		}
		return []*trillian.LogLeaf{NewLeafData (data)}, tx, nil
	} else {
		identifiers, identities, tx, err := reg.LogStorage.SearchUserMap (ctx, tree, key)
		if (err != nil) {
			return nil, nil, err
		}
		if len(identifiers) != len(identities) {
			panic("Invalid mapping between identifiers and identities.")
		}
		err = tx.DeleteFromUserMap (ctx, key)
		if (err != nil) {
			return nil, nil, err
		}
		leaves := make([]*trillian.LogLeaf, 0)
		for i, _ := range identifiers {
			identifier, identity := identifiers[i], identities[i]
			data, err := PrepareLeafData (newPk, identifier)
			if err != nil {
				return nil, nil, err
			}
			leaves = append (leaves, NewLeafData (data))
			contents :=  UserTypes.CreateMapContents (key.LogId, key.UserId, newPk, identifier, identity)
			err = tx.AddToUserMap (ctx, contents)
			if err != nil {
				return nil, nil, err
			}
		}
		return leaves, tx, nil
	}
}

func GetKeys (ctx context.Context, tree *trillian.Tree, reg extension.Registry, request *trillian.UserReadLeafRequest) ([]string, error) {
	keys, err := reg.LogStorage.GetKeys (ctx, tree, request)
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func arraysEqual (a[]byte, b[]byte) bool {
	if a == nil || b == nil {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func GetLatestLeaves (leaves []*trillian.LogLeaf) ([]*trillian.LogLeaf, error) {
	if leaves == nil {
		return nil, errors.New ("No leaves to reduce")
	}
	finalLeaves := make ([]*trillian.LogLeaf, 0)
	for i := 0; i < len (leaves); i++ {
		add := true
		for j := 0; j < len(leaves); j++ {
			if (arraysEqual(leaves[i].LeafIdentityHash, leaves[j].LeafIdentityHash) && (leaves[i].IntegrateTimestamp.Seconds > leaves[j].IntegrateTimestamp.Seconds || leaves[i].IntegrateTimestamp.Seconds == leaves[j].IntegrateTimestamp.Seconds && leaves[i].IntegrateTimestamp.Nanos > leaves[j].IntegrateTimestamp.Nanos)) {
				add = false
				break;
			}
		}
		if add {
			finalLeaves = append (finalLeaves, leaves[i])
		}
	}
	return finalLeaves, nil
}
