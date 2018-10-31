package UserMap

import (
	"context"
	"github.com/google.com/trillian"
)

func WriteTransaction(ctx context.Context, client *trillian.TrillianLogClient,
	logId int64, userData []*UserData, newPublicKey string) error {
	//GatherNodes(ctx, client, userData)

	//UpdateMap(userData, logId, newPublicKey)

	return nil
}
