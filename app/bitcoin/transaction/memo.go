package transaction

import (
	"bytes"
	"fmt"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcutil"
	"github.com/jchavannes/btcd/txscript"
	"github.com/jchavannes/jgo/jerr"
	"github.com/memocash/memo/app/bitcoin/memo"
	"github.com/memocash/memo/app/bitcoin/wallet"
	"github.com/memocash/memo/app/cache"
	"github.com/memocash/memo/app/db"
	"github.com/memocash/memo/app/html-parser"
	"github.com/memocash/memo/app/metric"
	"github.com/memocash/memo/app/profile/pic"
)

func GetMemoOutputIfExists(txn *db.Transaction) (*db.TransactionOut, error) {
	var out *db.TransactionOut
	for _, txOut := range txn.TxOut {
		if len(txOut.PkScript) < 5 || ! bytes.Equal(txOut.PkScript[0:3], []byte{
			txscript.OP_RETURN,
			txscript.OP_DATA_2,
			memo.CodePrefix,
		}) {
			continue
		}
		if out != nil {
			return nil, jerr.New("UNEXPECTED ERROR: found more than one memo in transaction")
		}
		out = txOut
	}
	return out, nil
}

func SaveMemo(txn *db.Transaction, out *db.TransactionOut, block *db.Block) error {
	inputAddress, err := getInputPkHash(txn)
	if err != nil {
		return jerr.Get("error getting pk hash from input", err)
	}
	// Used for ordering
	var parentHash []byte
	if len(txn.TxIn) == 1 {
		parentHash = txn.TxIn[0].PreviousOutPointHash
	}
	err, isNew := saveMemoTest(txn, out, block, inputAddress)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error saving memo_test", err)
	}
	memoCode := out.PkScript[3]
	switch memoCode {
	case memo.CodePost:
		err = saveMemoPost(txn, out, block, inputAddress, parentHash)
		if err != nil {
			return jerr.Get("error saving memo_post", err)
		}
	case memo.CodeSetName:
		err = saveMemoSetName(txn, out, block, inputAddress, parentHash)
		if err != nil {
			return jerr.Get("error saving memo_set_name", err)
		}
	case memo.CodeFollow:
		err = saveMemoFollow(txn, out, block, inputAddress, parentHash, false)
		if err != nil {
			return jerr.Get("error saving memo_follow", err)
		}
	case memo.CodeUnfollow:
		err = saveMemoFollow(txn, out, block, inputAddress, parentHash, true)
		if err != nil {
			return jerr.Get("error saving memo_follow", err)
		}
	case memo.CodeLike:
		err = saveMemoLike(txn, out, block, inputAddress, parentHash)
		if err != nil {
			return jerr.Get("error saving memo_like", err)
		}
	case memo.CodeReply:
		err = saveMemoReply(txn, out, block, inputAddress, parentHash)
		if err != nil {
			return jerr.Get("error saving memo_post reply", err)
		}
	case memo.CodeSetProfile:
		err = saveMemoSetProfile(txn, out, block, inputAddress, parentHash)
		if err != nil {
			return jerr.Get("error saving memo_set_profile", err)
		}
	case memo.CodeTopicMessage:
		err = saveMemoTopicMessage(txn, out, block, inputAddress, parentHash)
		if err != nil {
			return jerr.Get("error saving memo_post topic message", err)
		}
	case memo.CodeTopicFollow:
		err = saveMemoTopicFollow(false, txn, out, block, inputAddress, parentHash)
		if err != nil {
			return jerr.Get("error saving memo follow topic", err)
		}
	case memo.CodeTopicUnfollow:
		err = saveMemoTopicFollow(true, txn, out, block, inputAddress, parentHash)
		if err != nil {
			return jerr.Get("error saving memo unfollow topic", err)
		}
	case memo.CodePollCreate:
		err = saveMemoPollQuestion(txn, out, block, inputAddress, parentHash)
		if err != nil {
			return jerr.Get("error saving memo_poll_question (single)", err)
		}
	case memo.CodePollOption:
		err = saveMemoPollOption(txn, out, block, inputAddress, parentHash)
		if err != nil {
			return jerr.Get("error saving memo poll option", err)
		}
	case memo.CodePollVote:
		err = saveMemoPollVote(txn, out, block, inputAddress, parentHash)
		if err != nil {
			return jerr.Get("error saving memo poll vote", err)
		}
		err = saveMemoVotePost(txn, out, block, inputAddress, parentHash)
		if err != nil {
			return jerr.Get("error saving memo poll vote post", err)
		}
	case memo.CodeSetProfilePicture:
		err = saveMemoSetPic(txn, out, block, inputAddress, parentHash)
		if err != nil {
			return jerr.Get("error saving memo_set_pic", err)
		}
	}
	if isNew {
		go func() {
			err := metric.AddMemoSave(memoCode)
			if err != nil {
				jerr.Get("error adding memo save metric", err).Print()
			}
		}()
	}
	return nil
}

func saveMemoTest(txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash) (error, bool) {
	memoTest, err := db.GetMemoTest(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo_test", err), false
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoTest != nil {
		if memoTest.BlockId != 0 || blockId == 0 {
			return nil, false
		}
		memoTest.BlockId = blockId
		err = memoTest.Save()
		if err != nil {
			return jerr.Get("error saving memo_test", err), false
		}
		return nil, false
	}
	memoTest = &db.MemoTest{
		TxHash:   txn.Hash,
		PkHash:   inputAddress.ScriptAddress(),
		PkScript: out.PkScript,
		Address:  inputAddress.EncodeAddress(),
		BlockId:  blockId,
	}
	err = memoTest.Save()
	if err != nil {
		return jerr.Get("error saving memo_test", err), false
	}
	return nil, true
}

func saveMemoPost(txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash, parentHash []byte) error {
	memoPost, err := db.GetMemoPost(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo_post", err)
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoPost != nil {
		if memoPost.BlockId != 0 || blockId == 0 {
			return nil
		}
		memoPost.BlockId = blockId
		memoPost.Block = block
		err = memoPost.Save()
		if err != nil {
			return jerr.Get("error saving memo_post", err)
		}
		addMemoPostFeedEvent(memoPost)
		return nil
	}
	pushData, err := txscript.PushedData(out.PkScript)
	if err != nil {
		return jerr.Get("error parsing push data from message", err)
	}
	if len(pushData) != 2 {
		return jerr.Newf("invalid message, incorrect push data (%d)", len(pushData))
	}
	var message = string(pushData[1])
	memoPost = &db.MemoPost{
		TxHash:     txn.Hash,
		PkHash:     inputAddress.ScriptAddress(),
		PkScript:   out.PkScript,
		ParentHash: parentHash,
		Address:    inputAddress.EncodeAddress(),
		Message:    html_parser.EscapeWithEmojis(message),
		BlockId:    blockId,
		Block:      block,
	}
	err = memoPost.Save()
	if err != nil {
		return jerr.Get("error saving memo_post", err)
	}
	addMemoPostFeedEvent(memoPost)
	return nil
}

func saveMemoSetName(txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash, parentHash []byte) error {
	memoSetName, err := db.GetMemoSetName(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo_set_name", err)
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoSetName != nil {
		if memoSetName.BlockId != 0 || blockId == 0 {
			return nil
		}
		memoSetName.BlockId = blockId
		memoSetName.Block = block
		err = memoSetName.Save()
		if err != nil {
			return jerr.Get("error saving memo_set_name", err)
		}
		addMemoSetNameFeedEvent(memoSetName)
		return nil
	}
	pushData, err := txscript.PushedData(out.PkScript)
	if err != nil {
		return jerr.Get("error parsing push data from set name", err)
	}
	if len(pushData) != 2 {
		return jerr.Newf("invalid set name, incorrect push data (%d)", len(pushData))
	}
	var name = string(pushData[1])
	memoSetName = &db.MemoSetName{
		TxHash:     txn.Hash,
		PkHash:     inputAddress.ScriptAddress(),
		PkScript:   out.PkScript,
		ParentHash: parentHash,
		Address:    inputAddress.EncodeAddress(),
		Name:       html_parser.EscapeWithEmojis(name),
		BlockId:    blockId,
		Block:      block,
	}
	err = memoSetName.Save()
	if err != nil {
		return jerr.Get("error saving memo_set_name", err)
	}
	addMemoSetNameFeedEvent(memoSetName)
	return nil
}

func saveMemoSetPic(txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash, parentHash []byte) error {
	memoSetPic, err := db.GetMemoSetPic(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo_set_pic", err)
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoSetPic != nil {
		if memoSetPic.BlockId != 0 || blockId == 0 {
			return nil
		}
		memoSetPic.BlockId = blockId
		memoSetPic.Block = block
		err = memoSetPic.Save()
		if err != nil {
			return jerr.Get("error saving memo_set_pic", err)
		}
		addMemoSetProfilePicFeedEvent(memoSetPic)
		return nil
	}
	pushData, err := txscript.PushedData(out.PkScript)
	if err != nil {
		return jerr.Get("error parsing push data from set pic", err)
	}
	if len(pushData) != 2 {
		return jerr.Newf("invalid set pic, incorrect push data (%d)", len(pushData))
	}
	var url = html_parser.EscapeWithEmojis(string(pushData[1]))
	memoSetPic = &db.MemoSetPic{
		TxHash:     txn.Hash,
		PkHash:     inputAddress.ScriptAddress(),
		PkScript:   out.PkScript,
		ParentHash: parentHash,
		Address:    inputAddress.EncodeAddress(),
		Url:        url,
		BlockId:    blockId,
		Block:      block,
	}
	go func() {
		err = pic.FetchProfilePic(memoSetPic.Url, memoSetPic.GetAddressString())
		if err != nil {
			jerr.Get("error generating profile pic", err).Print()
		} else {
			fmt.Printf("Generated profile pic (%s) for user %s\n", memoSetPic.Url, memoSetPic.GetAddressString())
		}
		err = memoSetPic.Save()
		if err != nil {
			jerr.Get("error saving memo_set_pic", err).Print()
		}
		err = cache.ClearHasPic(inputAddress.ScriptAddress())
		if err != nil {
			jerr.Get("error clearing has pic cache", err).Print()
		}
	}()
	addMemoSetProfilePicFeedEvent(memoSetPic)
	return nil
}

func saveMemoFollow(txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash, parentHash []byte, unfollow bool) error {
	memoFollow, err := db.GetMemoFollow(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo_follow", err)
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoFollow != nil {
		if memoFollow.BlockId != 0 || blockId == 0 {
			return nil
		}
		memoFollow.BlockId = blockId
		memoFollow.Block = block
		err = memoFollow.Save()
		if err != nil {
			return jerr.Get("error saving memo_follow", err)
		}
		addMemoFollowFeedEvent(memoFollow)
		return nil
	}
	address := wallet.GetAddressFromPkHash(out.PkScript[5:])
	if ! bytes.Equal(address.GetScriptAddress(), out.PkScript[5:]) {
		return jerr.New("unable to parse follow address")
	}
	memoFollow = &db.MemoFollow{
		TxHash:       txn.Hash,
		PkHash:       inputAddress.ScriptAddress(),
		PkScript:     out.PkScript,
		ParentHash:   parentHash,
		Address:      inputAddress.EncodeAddress(),
		FollowPkHash: address.GetScriptAddress(),
		BlockId:      blockId,
		Block:        block,
		Unfollow:     unfollow,
	}
	err = memoFollow.Save()
	if err != nil {
		return jerr.Get("error saving memo_follow", err)
	}
	err = cache.ClearReputation(memoFollow.PkHash, memoFollow.FollowPkHash)
	if err != nil && ! cache.IsMissError(err) {
		return jerr.Get("error clearing cache", err)
	}
	if !unfollow {
		addFollowNotification(memoFollow)
	}
	addMemoFollowFeedEvent(memoFollow)
	return nil
}

func saveMemoLike(txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash, parentHash []byte) error {
	if len(out.PkScript) < 37 {
		return jerr.New("script too short")
	}
	memoLike, err := db.GetMemoLike(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo_like", err)
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoLike != nil {
		if memoLike.BlockId != 0 || blockId == 0 {
			return nil
		}
		memoLike.BlockId = blockId
		memoLike.Block = block
		err = memoLike.Save()
		if err != nil {
			return jerr.Get("error saving memo_like", err)
		}
		addMemoLikeFeedEvent(memoLike)
		return nil
	}

	txHash, err := chainhash.NewHash(out.PkScript[5:37])
	if err != nil {
		return jerr.Get("error parsing transaction hash", err)
	}
	var tipPkHash []byte
	var tipAmount int64
	for _, txOut := range txn.TxOut {
		if len(txOut.KeyPkHash) == 0 || bytes.Equal(txOut.KeyPkHash, inputAddress.ScriptAddress()) {
			continue
		}
		if len(tipPkHash) != 0 {
			return jerr.New("error found multiple tip outputs, unable to process")
		}
		tipAmount += txOut.Value
		tipPkHash = txOut.KeyPkHash
	}
	memoLike = &db.MemoLike{
		TxHash:     txn.Hash,
		PkHash:     inputAddress.ScriptAddress(),
		PkScript:   out.PkScript,
		ParentHash: parentHash,
		Address:    inputAddress.EncodeAddress(),
		LikeTxHash: txHash.CloneBytes(),
		BlockId:    blockId,
		Block:      block,
		TipPkHash:  tipPkHash,
		TipAmount:  tipAmount,
	}
	err = memoLike.Save()
	if err != nil {
		return jerr.Get("error saving memo_like", err)
	}
	addLikeNotification(memoLike)
	addMemoLikeFeedEvent(memoLike)
	return nil
}

func saveMemoReply(txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash, parentHash []byte) error {
	memoPost, err := db.GetMemoPost(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo_reply", err)
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoPost != nil {
		if memoPost.BlockId != 0 || blockId == 0 {
			return nil
		}
		memoPost.BlockId = blockId
		memoPost.Block = block
		err = memoPost.Save()
		if err != nil {
			return jerr.Get("error saving memo_reply", err)
		}
		addMemoPostFeedEvent(memoPost)
		return nil
	}
	if len(out.PkScript) < 38 {
		return jerr.Newf("invalid reply, length too short (%d)", len(out.PkScript))
	}
	pushData, err := txscript.PushedData(out.PkScript)
	if err != nil {
		return jerr.Get("error parsing push data from memo reply message", err)
	}
	if len(pushData) != 3 {
		return jerr.Newf("invalid reply message, incorrect push data (%d)", len(pushData))
	}
	var replyTxHash = pushData[1]
	var messageRaw = pushData[2]
	txHash, err := chainhash.NewHash(replyTxHash)
	if err != nil {
		return jerr.Get("error parsing transaction hash", err)
	}

	memoPost = &db.MemoPost{
		TxHash:       txn.Hash,
		PkHash:       inputAddress.ScriptAddress(),
		PkScript:     out.PkScript,
		ParentHash:   parentHash,
		Address:      inputAddress.EncodeAddress(),
		ParentTxHash: txHash.CloneBytes(),
		Message:      html_parser.EscapeWithEmojis(string(messageRaw)),
		BlockId:      blockId,
		Block:        block,
	}
	err = memoPost.Save()
	if err != nil {
		return jerr.Get("error saving memo_reply", err)
	}
	addReplyNotification(memoPost)
	updateRootTxHash(memoPost)
	addMemoPostFeedEvent(memoPost)
	return nil
}

func saveMemoTopicMessage(txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash, parentHash []byte) error {
	memoPost, err := db.GetMemoPost(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo topic message", err)
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoPost != nil {
		if memoPost.BlockId != 0 || blockId == 0 {
			return nil
		}
		memoPost.BlockId = blockId
		memoPost.Block = block
		err = memoPost.Save()
		if err != nil {
			return jerr.Get("error saving memo topic message", err)
		}
		addMemoPostFeedEvent(memoPost)
		return nil
	}

	pushData, err := txscript.PushedData(out.PkScript)
	if err != nil {
		return jerr.Get("error parsing push data from memo topic message", err)
	}
	if len(pushData) != 3 {
		return jerr.Newf("invalid topic message, incorrect push data (%d)", len(pushData))
	}
	var topicNameRaw = pushData[1]
	var messageRaw = pushData[2]
	if len(topicNameRaw) == 0 || len(messageRaw) == 0 {
		return jerr.Newf("empty topic or message (%d, %d)", len(topicNameRaw), len(messageRaw))
	}
	topicName := html_parser.EscapeWithEmojis(string(topicNameRaw))
	message := html_parser.EscapeWithEmojis(string(messageRaw))
	memoPost = &db.MemoPost{
		TxHash:     txn.Hash,
		PkHash:     inputAddress.ScriptAddress(),
		PkScript:   out.PkScript,
		ParentHash: parentHash,
		Address:    inputAddress.EncodeAddress(),
		Topic:      topicName,
		Message:    message,
		BlockId:    blockId,
		Block:      block,
	}
	err = memoPost.Save()
	if err != nil {
		return jerr.Get("error saving memo topic message", err)
	}
	addMemoPostFeedEvent(memoPost)
	updateTopicInfo(topicName)
	return nil
}

func saveMemoTopicFollow(unfollow bool, txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash, parentHash []byte) error {
	memoFollowTopic, err := db.GetMemoTopicFollow(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo follow topic", err)
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoFollowTopic != nil {
		if memoFollowTopic.BlockId != 0 || blockId == 0 {
			return nil
		}
		memoFollowTopic.BlockId = blockId
		memoFollowTopic.Block = block
		err = memoFollowTopic.Save()
		if err != nil {
			return jerr.Get("error saving memo follow topic", err)
		}
		addMemoTopicFollowFeedEvent(memoFollowTopic)
		return nil
	}

	pushData, err := txscript.PushedData(out.PkScript)
	if err != nil {
		return jerr.Get("error parsing push data from memo follow topic", err)
	}
	if len(pushData) != 2 {
		return jerr.Newf("invalid topic follow, incorrect push data (%d)", len(pushData))
	}
	var topicNameRaw = pushData[1]
	if len(topicNameRaw) == 0 {
		return jerr.Newf("empty topic follow name (%d)", len(topicNameRaw))
	}
	topicName := html_parser.EscapeWithEmojis(string(topicNameRaw))
	memoFollowTopic = &db.MemoTopicFollow{
		TxHash:     txn.Hash,
		PkHash:     inputAddress.ScriptAddress(),
		PkScript:   out.PkScript,
		ParentHash: parentHash,
		Topic:      topicName,
		BlockId:    blockId,
		Block:      block,
		Unfollow:   unfollow,
	}
	err = memoFollowTopic.Save()
	if err != nil {
		return jerr.Get("error saving memo follow topic", err)
	}
	addMemoTopicFollowFeedEvent(memoFollowTopic)
	updateTopicInfo(topicName)
	return nil
}

func saveMemoSetProfile(txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash, parentHash []byte) error {
	memoSetProfile, err := db.GetMemoSetProfile(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo_set_profile", err)
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoSetProfile != nil {
		if memoSetProfile.BlockId != 0 || blockId == 0 {
			return nil
		}
		memoSetProfile.BlockId = blockId
		memoSetProfile.Block = block
		err = memoSetProfile.Save()
		if err != nil {
			return jerr.Get("error saving memo_set_profile", err)
		}
		addMemoSetProfileFeedEvent(memoSetProfile)
		return nil
	}
	pushData, err := txscript.PushedData(out.PkScript)
	if err != nil {
		return jerr.Get("error parsing push data from profile text", err)
	}
	if len(pushData) != 2 {
		return jerr.Newf("invalid profile text, incorrect push data (%d)", len(pushData))
	}
	var profile = string(pushData[1])
	memoSetProfile = &db.MemoSetProfile{
		TxHash:     txn.Hash,
		PkHash:     inputAddress.ScriptAddress(),
		PkScript:   out.PkScript,
		ParentHash: parentHash,
		Address:    inputAddress.EncodeAddress(),
		Profile:    html_parser.EscapeWithEmojis(profile),
		BlockId:    blockId,
		Block:      block,
	}
	err = memoSetProfile.Save()
	if err != nil {
		return jerr.Get("error saving memo_set_profile", err)
	}
	addMemoSetProfileFeedEvent(memoSetProfile)
	return nil
}

func saveMemoPollQuestion(txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash, parentHash []byte) error {
	memoPost, err := db.GetMemoPost(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo_post", err)
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoPost != nil {
		if memoPost.BlockId != 0 || blockId == 0 {
			return nil
		}
		memoPost.BlockId = blockId
		memoPost.Block = block
		err = memoPost.Save()
		if err != nil {
			return jerr.Get("error saving memo_poll_question", err)
		}
		addMemoPostFeedEvent(memoPost)
		return nil
	}
	pushData, err := txscript.PushedData(out.PkScript)
	if err != nil {
		return jerr.Get("error parsing push data from poll question", err)
	}
	if len(pushData) < 3 {
		return jerr.Newf("invalid poll question, incorrect push data (%d)", len(pushData))
	}
	var pollType = memo.CodePollTypeSingle
	if len(pushData) == 4 {
		pollType = int(pushData[1][0])
		pushData = pushData[1:]
	}
	if len(pushData[1]) == 0 {
		return jerr.New("invalid push data for poll question, num options empty")
	}
	if len(pushData[2]) == 0 {
		return jerr.New("invalid push data for poll question, question empty")
	}
	var numOptions = uint(pushData[1][0])
	var question = string(pushData[2])
	memoPost = &db.MemoPost{
		TxHash:     txn.Hash,
		PkHash:     inputAddress.ScriptAddress(),
		PkScript:   out.PkScript,
		Message:    html_parser.EscapeWithEmojis(question),
		ParentHash: parentHash,
		Address:    inputAddress.EncodeAddress(),
		BlockId:    blockId,
		Block:      block,
		IsPoll:     true,
	}
	err = memoPost.Save()
	if err != nil {
		return jerr.Get("error saving memo_post for poll question", err)
	}
	memoPollQuestion := &db.MemoPollQuestion{
		TxHash:     txn.Hash,
		NumOptions: numOptions,
		PollType:   pollType,
	}
	err = memoPollQuestion.Save()
	if err != nil {
		return jerr.Get("error saving memo_set_profile", err)
	}
	addMemoPostFeedEvent(memoPost)
	return nil
}

func saveMemoPollOption(txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash, parentHash []byte) error {
	memoPollOption, err := db.GetMemoPollOption(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo_poll_option", err)
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoPollOption != nil {
		if memoPollOption.BlockId != 0 || blockId == 0 {
			return nil
		}
		memoPollOption.BlockId = blockId
		memoPollOption.Block = block
		err = memoPollOption.Save()
		if err != nil {
			return jerr.Get("error saving memo_poll_option", err)
		}
		return nil
	}
	pushData, err := txscript.PushedData(out.PkScript)
	if err != nil {
		return jerr.Get("error parsing push data from poll option", err)
	}
	if len(pushData) != 3 {
		return jerr.Newf("invalid poll option, incorrect push data (%d)", len(pushData))
	}
	var pollTxHashRaw = pushData[1]
	if len(pollTxHashRaw) == 0 {
		return jerr.New("invalid push data for poll option, parent tx hash empty")
	}
	pollTxHash, err := chainhash.NewHash(pollTxHashRaw)
	if err != nil {
		return jerr.Get("error parsing transaction hash", err)
	}
	var option = string(pushData[2])
	if len(option) == 0 {
		return jerr.New("invalid push data for poll option, option empty")
	}
	memoPollOption = &db.MemoPollOption{
		TxHash:     txn.Hash,
		PkHash:     inputAddress.ScriptAddress(),
		PkScript:   out.PkScript,
		Option:     html_parser.EscapeWithEmojis(option),
		PollTxHash: pollTxHash.CloneBytes(),
		ParentHash: parentHash,
		BlockId:    blockId,
		Block:      block,
	}
	err = memoPollOption.Save()
	if err != nil {
		return jerr.Get("error saving memo_post for poll option", err)
	}
	return nil
}

func saveMemoPollVote(txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash, parentHash []byte) error {
	memoPollVote, err := db.GetMemoPollVote(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo_poll_vote", err)
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoPollVote != nil {
		if memoPollVote.BlockId != 0 || blockId == 0 {
			return nil
		}
		memoPollVote.BlockId = blockId
		memoPollVote.Block = block
		err = memoPollVote.Save()
		if err != nil {
			return jerr.Get("error saving memo_poll_vote", err)
		}
		addMemoPollVoteFeedEvent(memoPollVote)
		return nil
	}
	pushData, err := txscript.PushedData(out.PkScript)
	if err != nil {
		return jerr.Get("error parsing push data from poll vote", err)
	}
	if len(pushData) < 2 {
		return jerr.Newf("invalid poll vote, incorrect push data (%d)", len(pushData))
	}
	var optionTxHashRaw = pushData[1]
	if len(optionTxHashRaw) == 0 {
		return jerr.New("invalid push data for poll vote, option tx hash empty")
	}
	optionTxHash, err := chainhash.NewHash(optionTxHashRaw)
	if err != nil {
		return jerr.Get("error parsing option transaction hash", err)
	}
	var message string
	if len(pushData) == 3 {
		message = string(pushData[2])
	}
	var tipPkHash []byte
	var tipAmount int64
	for _, txOut := range txn.TxOut {
		if len(txOut.KeyPkHash) == 0 || bytes.Equal(txOut.KeyPkHash, inputAddress.ScriptAddress()) {
			continue
		}
		if len(tipPkHash) != 0 {
			return jerr.New("error found multiple tip outputs, unable to process")
		}
		tipAmount += txOut.Value
		tipPkHash = txOut.KeyPkHash
	}
	memoPollVote = &db.MemoPollVote{
		TxHash:       txn.Hash,
		PkHash:       inputAddress.ScriptAddress(),
		PkScript:     out.PkScript,
		Message:      html_parser.EscapeWithEmojis(message),
		OptionTxHash: optionTxHash.CloneBytes(),
		TipAmount:    tipAmount,
		TipPkHash:    tipPkHash,
		ParentHash:   parentHash,
		BlockId:      blockId,
		Block:        block,
	}
	err = memoPollVote.Save()
	if err != nil {
		return jerr.Get("error saving memo_post for poll vote", err)
	}
	addMemoPollVoteFeedEvent(memoPollVote)
	return nil
}

func saveMemoVotePost(txn *db.Transaction, out *db.TransactionOut, block *db.Block, inputAddress *btcutil.AddressPubKeyHash, parentHash []byte) error {
	memoPost, err := db.GetMemoPost(txn.Hash)
	if err != nil && ! db.IsRecordNotFoundError(err) {
		return jerr.Get("error getting memo_post for poll vote", err)
	}
	var blockId uint
	if block != nil {
		blockId = block.Id
	}
	if memoPost != nil {
		if memoPost.BlockId != 0 || blockId == 0 {
			return nil
		}
		memoPost.BlockId = blockId
		memoPost.Block = block
		err = memoPost.Save()
		if err != nil {
			return jerr.Get("error saving memo_post for poll vote", err)
		}
		addMemoPostFeedEvent(memoPost)
		return nil
	}
	pushData, err := txscript.PushedData(out.PkScript)
	if err != nil {
		return jerr.Get("error parsing push data from poll vote", err)
	}
	if len(pushData) < 2 {
		return jerr.Newf("invalid poll vote, incorrect push data (%d)", len(pushData))
	}
	var message string
	if len(pushData) == 3 {
		message = string(pushData[2])
	}
	if message == "" {
		return nil
	}
	memoPost = &db.MemoPost{
		TxHash:     txn.Hash,
		PkHash:     inputAddress.ScriptAddress(),
		PkScript:   out.PkScript,
		Message:    html_parser.EscapeWithEmojis(message),
		ParentHash: parentHash,
		Address:    inputAddress.EncodeAddress(),
		BlockId:    blockId,
		Block:      block,
		IsVote:     true,
	}
	err = memoPost.Save()
	if err != nil {
		return jerr.Get("error saving memo_post for poll vote", err)
	}
	addMemoPostFeedEvent(memoPost)
	return nil
}

func getInputPkHash(txn *db.Transaction) (*btcutil.AddressPubKeyHash, error) {
	var pkHash []byte
	for _, in := range txn.TxIn {
		tmpPkHash := in.GetAddress().GetScriptAddress()
		if len(tmpPkHash) > 0 {
			if len(pkHash) != 0 && ! bytes.Equal(tmpPkHash, pkHash) {
				return nil, jerr.New("error found multiple addresses in inputs")
			}
			pkHash = tmpPkHash
		}
	}
	if len(pkHash) == 0 {
		// Unknown script type
		return nil, jerr.New("error no pk hash found")
	}
	addressPkHash, err := btcutil.NewAddressPubKeyHash(pkHash, &wallet.MainNetParamsOld)
	if err != nil {
		return nil, jerr.Get("error getting pubkeyhash from memo test", err)
	}
	return addressPkHash, nil
}
