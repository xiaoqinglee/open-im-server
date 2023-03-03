package unrelation

import (
	"OpenIM/pkg/common/constant"
	"OpenIM/pkg/proto/sdkws"
	"context"
	"strconv"
	"strings"
)

const (
	singleGocMsgNum = 5000
	CChat           = "msg"
	OldestList      = 0
	NewestList      = -1
)

type MsgDocModel struct {
	DocID string         `bson:"uid"`
	Msg   []MsgInfoModel `bson:"msg"`
}

type MsgInfoModel struct {
	SendTime int64  `bson:"sendtime"`
	Msg      []byte `bson:"msg"`
}

type MsgDocModelInterface interface {
	PushMsgsToDoc(ctx context.Context, docID string, msgsToMongo []MsgInfoModel) error
	Create(ctx context.Context, model *MsgDocModel) error
	UpdateMsgStatusByIndexInOneDoc(ctx context.Context, docID string, msg *sdkws.MsgData, seqIndex int, status int32) error
	FindOneByDocID(ctx context.Context, docID string) (*MsgDocModel, error)
	GetNewestMsg(ctx context.Context, sourceID string) (*MsgInfoModel, error)
	GetOldestMsg(ctx context.Context, sourceID string) (*MsgInfoModel, error)
	Delete(ctx context.Context, docIDs []string) error
	GetMsgsByIndex(ctx context.Context, sourceID string, index int64) (*MsgDocModel, error)
	UpdateOneDoc(ctx context.Context, msg *MsgDocModel) error
}

func (MsgDocModel) TableName() string {
	return CChat
}

func (MsgDocModel) GetSingleGocMsgNum() int64 {
	return singleGocMsgNum
}

func (m *MsgDocModel) IsFull() bool {
	index, _ := strconv.Atoi(strings.Split(m.DocID, ":")[1])
	if index == 0 {
		if len(m.Msg) >= singleGocMsgNum-1 {
			return true
		}
	}
	if len(m.Msg) >= singleGocMsgNum {
		return true
	}

	return false
}

func (m MsgDocModel) GetDocID(sourceID string, seq int64) string {
	seqSuffix := seq / singleGocMsgNum
	return m.indexGen(sourceID, seqSuffix)
}

func (m MsgDocModel) GetSeqDocIDList(userID string, maxSeq int64) []string {
	seqMaxSuffix := maxSeq / singleGocMsgNum
	var seqUserIDs []string
	for i := 0; i <= int(seqMaxSuffix); i++ {
		seqUserID := m.indexGen(userID, int64(i))
		seqUserIDs = append(seqUserIDs, seqUserID)
	}
	return seqUserIDs
}

func (m MsgDocModel) getSeqSuperGroupID(groupID string, seq int64) string {
	seqSuffix := seq / singleGocMsgNum
	return m.superGroupIndexGen(groupID, seqSuffix)
}

func (m MsgDocModel) superGroupIndexGen(groupID string, seqSuffix int64) string {
	return "super_group_" + groupID + ":" + strconv.FormatInt(int64(seqSuffix), 10)
}

func (m MsgDocModel) GetDocIDSeqsMap(sourceID string, seqs []int64) map[string][]int64 {
	t := make(map[string][]int64)
	for i := 0; i < len(seqs); i++ {
		docID := m.GetDocID(sourceID, seqs[i])
		if value, ok := t[docID]; !ok {
			var temp []int64
			t[docID] = append(temp, seqs[i])
		} else {
			t[docID] = append(value, seqs[i])
		}
	}
	return t
}

func (m MsgDocModel) getMsgIndex(seq uint32) int {
	seqSuffix := seq / singleGocMsgNum
	var index uint32
	if seqSuffix == 0 {
		index = (seq - seqSuffix*singleGocMsgNum) - 1
	} else {
		index = seq - seqSuffix*singleGocMsgNum
	}
	return int(index)
}

func (m MsgDocModel) indexGen(sourceID string, seqSuffix int64) string {
	return sourceID + ":" + strconv.FormatInt(seqSuffix, 10)
}

func (MsgDocModel) GenExceptionMessageBySeqs(seqs []int64) (exceptionMsg []*sdkws.MsgData) {
	for _, v := range seqs {
		msg := new(sdkws.MsgData)
		msg.Seq = v
		exceptionMsg = append(exceptionMsg, msg)
	}
	return exceptionMsg
}

func (MsgDocModel) GenExceptionSuperGroupMessageBySeqs(seqs []int64, groupID string) (exceptionMsg []*sdkws.MsgData) {
	for _, v := range seqs {
		msg := new(sdkws.MsgData)
		msg.Seq = v
		msg.GroupID = groupID
		msg.SessionType = constant.SuperGroupChatType
		exceptionMsg = append(exceptionMsg, msg)
	}
	return exceptionMsg
}