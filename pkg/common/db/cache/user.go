package cache

import (
	"context"
	"encoding/json"
	"github.com/OpenIMSDK/Open-IM-Server/pkg/common/db/relation"
	relationTb "github.com/OpenIMSDK/Open-IM-Server/pkg/common/db/table/relation"
	"github.com/OpenIMSDK/Open-IM-Server/pkg/utils"
	"github.com/dtm-labs/rockscache"
	"github.com/go-redis/redis/v8"
	"strconv"
	"time"
)

const (
	userExpireTime          = time.Second * 60 * 60 * 12
	userInfoKey             = "USER_INFO:"
	userGlobalRecvMsgOptKey = "USER_GLOBAL_RECV_MSG_OPT_KEY:"
)

type UserCache interface {
}

type UserCacheRedis struct {
	userDB *relation.UserGorm

	expireTime time.Duration

	rcClient *rockscache.Client
}

func NewUserCacheRedis(rdb redis.UniversalClient, userDB *relation.UserGorm, options rockscache.Options) *UserCacheRedis {
	return &UserCacheRedis{
		userDB:     userDB,
		expireTime: userExpireTime,
		rcClient:   rockscache.NewClient(rdb, options),
	}
}

func (u *UserCacheRedis) getUserInfoKey(userID string) string {
	return userInfoKey + userID
}

func (u *UserCacheRedis) getUserGlobalRecvMsgOptKey(userID string) string {
	return userGlobalRecvMsgOptKey + userID
}

func (u *UserCacheRedis) GetUserInfo(ctx context.Context, userID string) (userInfo *relationTb.UserModel, err error) {
	getUserInfo := func() (string, error) {
		userInfo, err := u.userDB.Take(ctx, userID)
		if err != nil {
			return "", err
		}
		bytes, err := json.Marshal(userInfo)
		if err != nil {
			return "", utils.Wrap(err, "")
		}
		return string(bytes), nil
	}
	userInfoStr, err := u.rcClient.Fetch(u.getUserInfoKey(userID), u.expireTime, getUserInfo)
	if err != nil {
		return nil, err
	}
	userInfo = &relationTb.UserModel{}
	err = json.Unmarshal([]byte(userInfoStr), userInfo)
	return userInfo, utils.Wrap(err, "")
}

func (u *UserCacheRedis) GetUsersInfo(ctx context.Context, userIDs []string) ([]*relationTb.UserModel, error) {
	var users []*relationTb.UserModel
	//for _, userID := range userIDs {
	//	user, err := GetUserInfoFromCache(ctx, userID)
	//	if err != nil {
	//		return nil, err
	//	}
	//	users = append(users, user)
	//}
	return users, nil
}

func (u *UserCacheRedis) DelUserInfo(ctx context.Context, userID string) (err error) {
	return u.rcClient.TagAsDeleted(u.getUserInfoKey(userID))
}

func (u *UserCacheRedis) DelUsersInfo(ctx context.Context, userIDs []string) (err error) {
	for _, userID := range userIDs {
		if err := u.DelUserInfo(ctx, userID); err != nil {
			return err
		}
	}
	return nil
}

func (u *UserCacheRedis) GetUserGlobalRecvMsgOpt(ctx context.Context, userID string) (opt int, err error) {
	getUserGlobalRecvMsgOpt := func() (string, error) {
		userInfo, err := u.userDB.Take(ctx, userID)
		if err != nil {
			return "", err
		}
		return strconv.Itoa(int(userInfo.GlobalRecvMsgOpt)), nil
	}
	optStr, err := u.rcClient.Fetch(u.getUserInfoKey(userID), u.expireTime, getUserGlobalRecvMsgOpt)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(optStr)
}

func (u *UserCacheRedis) DelUserGlobalRecvMsgOpt(ctx context.Context, userID string) (err error) {
	return u.rcClient.TagAsDeleted(u.getUserGlobalRecvMsgOptKey(userID))
}