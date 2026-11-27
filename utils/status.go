package utils

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"go.mau.fi/libsignal/groups"
	"go.mau.fi/libsignal/protocol"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// ApplyStatusReshareable enables WhatsApp's "Allow Reshare" button on status
// updates (Text, Image, Video, Audio) via ContextInfo.FeatureEligibilities.CanBeReshared.
func ApplyStatusReshareable(msg *waE2E.Message) {
	if msg == nil || msg.ProtocolMessage != nil {
		return
	}
	setFlag := func(ci *waE2E.ContextInfo) *waE2E.ContextInfo {
		if ci == nil {
			ci = &waE2E.ContextInfo{}
		}
		if ci.FeatureEligibilities == nil {
			ci.FeatureEligibilities = &waE2E.ContextInfo_FeatureEligibilities{}
		}
		ci.FeatureEligibilities.CanBeReshared = proto.Bool(true)
		return ci
	}
	if msg.ExtendedTextMessage != nil {
		msg.ExtendedTextMessage.ContextInfo = setFlag(msg.ExtendedTextMessage.ContextInfo)
	}
	if msg.ImageMessage != nil {
		msg.ImageMessage.ContextInfo = setFlag(msg.ImageMessage.ContextInfo)
	}
	if msg.VideoMessage != nil {
		msg.VideoMessage.ContextInfo = setFlag(msg.VideoMessage.ContextInfo)
	}
	if msg.AudioMessage != nil {
		msg.AudioMessage.ContextInfo = setFlag(msg.AudioMessage.ContextInfo)
	}
}

// WrapDeviceForStatusBroadcast is kept as a no-op hook for backwards compatibility;
// SendStatusBroadcast now routes status@broadcast natively via LID (whatsmeow PR #1294).
func WrapDeviceForStatusBroadcast(device *store.Device) {}

// ResolveLIDStatusRecipients builds a pure @lid recipient list matching WhatsApp's
// current status@broadcast protocol across ALL 3 privacy modes:
//   - StatusPrivacyTypeContacts ("My Contacts")
//   - StatusPrivacyTypeBlacklist ("My Contacts Except...")
//   - StatusPrivacyTypeWhitelist ("Only Share With...")
func ResolveLIDStatusRecipients(
	ctx context.Context,
	ownLID waTypes.JID,
	priv waTypes.StatusPrivacy,
	contacts map[waTypes.JID]waTypes.ContactInfo,
	lids store.LIDStore,
) ([]waTypes.JID, error) {
	var candidates []waTypes.JID
	if priv.Type == waTypes.StatusPrivacyTypeWhitelist {
		candidates = priv.List
	} else {
		for jid, info := range contacts {
			if len(info.FullName) > 0 {
				candidates = append(candidates, jid)
			}
		}
	}

	// Gather all PN JIDs from candidates and blacklist for a single batch LID lookup.
	pnSeen := make(map[waTypes.JID]struct{})
	var pnList []waTypes.JID
	addPN := func(j waTypes.JID) {
		c := j.ToNonAD()
		if c.Server == waTypes.DefaultUserServer {
			if _, ok := pnSeen[c]; !ok {
				pnSeen[c] = struct{}{}
				pnList = append(pnList, c)
			}
		}
	}
	for _, j := range candidates {
		addPN(j)
	}
	if priv.Type == waTypes.StatusPrivacyTypeBlacklist {
		for _, j := range priv.List {
			addPN(j)
		}
	}

	lidMap := make(map[waTypes.JID]waTypes.JID)
	if lids != nil && len(pnList) > 0 {
		if m, err := lids.GetManyLIDsForPNs(ctx, pnList); err == nil && m != nil {
			lidMap = m
		}
	}

	// Build blacklist matching BOTH PN and LID so excluded contacts can never leak through.
	blacklist := make(map[waTypes.JID]struct{})
	if priv.Type == waTypes.StatusPrivacyTypeBlacklist {
		for _, raw := range priv.List {
			base := raw.ToNonAD()
			blacklist[base] = struct{}{}
			if base.Server == waTypes.DefaultUserServer {
				if mapped, ok := lidMap[base]; ok && !mapped.IsEmpty() {
					blacklist[mapped.ToNonAD()] = struct{}{}
				}
			} else if base.Server == waTypes.HiddenUserServer && lids != nil {
				if pn, err := lids.GetPNForLID(ctx, base); err == nil && !pn.IsEmpty() {
					blacklist[pn.ToNonAD()] = struct{}{}
				}
			}
		}
	}

	// Normalize every eligible recipient to @lid, deduplicated.
	seen := make(map[waTypes.JID]struct{}, len(candidates)+1)
	recipients := make([]waTypes.JID, 0, len(candidates)+1)
	for _, raw := range candidates {
		base := raw.ToNonAD()
		target := base
		if base.Server == waTypes.DefaultUserServer {
			target = lidMap[base].ToNonAD()
		}
		if target.IsEmpty() || target.Server != waTypes.HiddenUserServer {
			continue
		}
		if _, blocked := blacklist[base]; blocked {
			continue
		}
		if _, blocked := blacklist[target]; blocked {
			continue
		}
		if _, dup := seen[target]; !dup {
			seen[target] = struct{}{}
			recipients = append(recipients, target)
		}
	}

	// Always include own LID (required by WhatsApp so the status appears on your own phone).
	ownClean := ownLID.ToNonAD()
	if !ownClean.IsEmpty() && ownClean.Server == waTypes.HiddenUserServer {
		if _, dup := seen[ownClean]; !dup {
			recipients = append(recipients, ownClean)
		}
	}
	return recipients, nil
}

func isHostedStatusDevice(j waTypes.JID) bool {
	return j.Server == waTypes.HostedServer ||
		j.Server == waTypes.HostedLIDServer ||
		j.Device == 99
}

func filterLIDStatusDevices(devices []waTypes.JID) []waTypes.JID {
	return slices.DeleteFunc(slices.Clone(devices), func(j waTypes.JID) bool {
		return j.Server != waTypes.HiddenUserServer || isHostedStatusDevice(j)
	})
}

type statusDeviceKey struct {
	User   string
	Server string
	Device uint16
}

// mergeStatusDevices combines filtered contact LID devices with all of the
// sender's own devices projected onto ownLID.
//
// WhatsApp's usync device query frequently returns empty or PN-only entries
// when querying your own @lid directly, causing the user's primary phone
// (ownLID:0@lid) to be omitted from <participants> so companion-posted statuses
// never appear under "My Status". Projecting own devices (fetched via own PN)
// onto ownLID and unconditionally guaranteeing primary device 0 ensures the
// status always appears on the owner's phone and other linked devices.
func mergeStatusDevices(contactRawDevices, ownRawDevices []waTypes.JID, ownLID waTypes.JID) (allDevices []waTypes.JID, ownTargetDeviceIDs []uint16) {
	seen := make(map[statusDeviceKey]struct{}, len(contactRawDevices)+len(ownRawDevices)+2)
	add := func(j waTypes.JID) {
		if j.User == "" || j.Server != waTypes.HiddenUserServer || isHostedStatusDevice(j) {
			return
		}
		k := statusDeviceKey{User: j.User, Server: j.Server, Device: j.Device}
		if _, exists := seen[k]; exists {
			return
		}
		seen[k] = struct{}{}
		allDevices = append(allDevices, waTypes.JID{
			User:   j.User,
			Device: j.Device,
			Server: waTypes.HiddenUserServer,
		})
	}

	for _, d := range contactRawDevices {
		if d.Server == waTypes.HiddenUserServer && !isHostedStatusDevice(d) {
			add(d)
		}
	}

	if !ownLID.IsEmpty() && ownLID.Server == waTypes.HiddenUserServer {
		// Unconditionally include primary phone (Device 0) so the status always
		// appears under "My Status" on the owner's main phone.
		add(waTypes.JID{
			User:   ownLID.User,
			Device: 0,
			Server: waTypes.HiddenUserServer,
		})
		// Project every discovered own device (from PN or LID usync) onto ownLID.
		for _, d := range ownRawDevices {
			if isHostedStatusDevice(d) {
				continue
			}
			add(waTypes.JID{
				User:   ownLID.User,
				Device: d.Device,
				Server: waTypes.HiddenUserServer,
			})
		}
		// Include the sending device itself so phash covers the full device set
		// (EncryptMessageForDevices skips ownLID automatically when encrypting SKDMs).
		add(waTypes.JID{
			User:   ownLID.User,
			Device: ownLID.Device,
			Server: waTypes.HiddenUserServer,
		})

		for _, d := range allDevices {
			if d.User == ownLID.User && d.Device != ownLID.Device {
				ownTargetDeviceIDs = append(ownTargetDeviceIDs, d.Device)
			}
		}
		slices.Sort(ownTargetDeviceIDs)
	}
	return allDevices, ownTargetDeviceIDs
}

func statusPrivacySettingAttr(privType waTypes.StatusPrivacyType) string {
	switch privType {
	case waTypes.StatusPrivacyTypeWhitelist:
		return "allowlist"
	case waTypes.StatusPrivacyTypeBlacklist:
		return "denylist"
	default:
		return "contacts"
	}
}

func statusParticipantListHashV2(participants []waTypes.JID) string {
	strs := make([]string, len(participants))
	for i, p := range participants {
		strs[i] = p.ADString()
	}
	sort.Strings(strs)
	hash := sha256.Sum256([]byte(strings.Join(strs, "")))
	return fmt.Sprintf("2:%s", base64.RawStdEncoding.EncodeToString(hash[:6]))
}

func padStatusPlaintext(plaintext []byte) []byte {
	var b [1]byte
	_, _ = rand.Read(b[:])
	pad := b[0] & 0xf
	if pad == 0 {
		pad = 0xf
	}
	return append(plaintext, bytes.Repeat([]byte{pad}, int(pad))...)
}

func statusMediaType(msg *waE2E.Message) string {
	switch {
	case msg.ImageMessage != nil:
		return "image"
	case msg.VideoMessage != nil:
		if msg.VideoMessage.GetGifPlayback() {
			return "gif"
		}
		return "video"
	case msg.AudioMessage != nil:
		if msg.AudioMessage.GetPTT() {
			return "ptt"
		}
		return "audio"
	case msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.Title != nil:
		return "url"
	default:
		return ""
	}
}

// SendStatusBroadcast posts (or revokes) a WhatsApp Status (Story) to
// status@broadcast routed strictly via @lid (fixing tulir/whatsmeow#1293 / PR #1294
// across My Contacts, My Contacts Except..., and Only Share With...) and marks
// statuses as reshareable (CanBeReshared=true).
func SendStatusBroadcast(ctx context.Context, client *whatsmeow.Client, msg *waE2E.Message) (whatsmeow.SendResponse, error) {
	if client == nil || client.Store == nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("client not initialized")
	}
	ApplyStatusReshareable(msg)

	internals := client.DangerousInternals()
	ownLID := internals.GetOwnLID()
	if ownLID.IsEmpty() || ownLID.Server != waTypes.HiddenUserServer {
		// Fallback for legacy non-LID accounts.
		return client.SendMessage(ctx, waTypes.StatusBroadcastJID, msg)
	}

	// 1. Resolve status privacy + LID-only recipients (supports Contacts, Blacklist, and Whitelist).
	privOpts, err := client.GetStatusPrivacy(ctx)
	if err != nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("failed to get status privacy: %w", err)
	}
	priv := whatsmeow.DefaultStatusPrivacy[0]
	if len(privOpts) > 0 {
		priv = privOpts[0]
	}

	var allContacts map[waTypes.JID]waTypes.ContactInfo
	if priv.Type != waTypes.StatusPrivacyTypeWhitelist && client.Store.Contacts != nil {
		allContacts, err = client.Store.Contacts.GetAllContacts(ctx)
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to read contacts: %w", err)
		}
	}

	recipients, err := ResolveLIDStatusRecipients(ctx, ownLID, priv, allContacts, client.Store.LIDs)
	if err != nil {
		return whatsmeow.SendResponse{}, err
	}
	if len(recipients) == 0 {
		return whatsmeow.SendResponse{}, fmt.Errorf("no eligible LID recipients found for status privacy=%s", priv.Type)
	}

	// 2. Ensure MessageSecret when reporting token is required.
	if internals.ShouldIncludeReportingToken(msg) {
		if msg.MessageContextInfo == nil {
			msg.MessageContextInfo = &waE2E.MessageContextInfo{}
		}
		if len(msg.MessageContextInfo.MessageSecret) == 0 {
			sec := make([]byte, 32)
			_, _ = rand.Read(sec)
			msg.MessageContextInfo.MessageSecret = sec
		}
	}

	// 3. Marshal & encrypt payload using Signal SenderKey bound to ownLID.
	to := waTypes.StatusBroadcastJID
	msgID := client.GenerateMessageID()
	plaintext, err := proto.Marshal(msg)
	if err != nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("marshal status message: %w", err)
	}

	builder := groups.NewGroupSessionBuilder(client.Store, store.SignalProtobufSerializer)
	senderKeyName := protocol.NewSenderKeyName(to.String(), ownLID.SignalAddress())
	signalSKD, err := builder.Create(ctx, senderKeyName)
	if err != nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("create sender key for status: %w", err)
	}
	skdPlaintext, err := proto.Marshal(&waE2E.Message{
		SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{
			GroupID:                             proto.String(to.String()),
			AxolotlSenderKeyDistributionMessage: signalSKD.Serialize(),
		},
	})
	if err != nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("marshal SKDM for status: %w", err)
	}

	cipher := groups.NewGroupCipher(builder, senderKeyName, client.Store)
	encrypted, err := cipher.Encrypt(ctx, padStatusPlaintext(plaintext))
	if err != nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("encrypt status payload: %w", err)
	}
	ciphertext := encrypted.SignedSerialize()

	// 4. Resolve recipient devices:
	//    - Third-party contacts are queried via @lid (PN usync returns empty for LID contacts).
	//    - Own devices are queried via own PN (@s.whatsapp.net) + own LID and projected onto
	//      ownLID with primary device 0 guaranteed, so the status always appears under "My Status".
	contactRecipients := make([]waTypes.JID, 0, len(recipients))
	for _, r := range recipients {
		if r.User != ownLID.User {
			contactRecipients = append(contactRecipients, r)
		}
	}

	var contactRawDevices []waTypes.JID
	if len(contactRecipients) > 0 {
		contactRawDevices, err = client.GetUserDevices(ctx, contactRecipients)
		if err != nil {
			return whatsmeow.SendResponse{}, fmt.Errorf("failed to get status recipient devices: %w", err)
		}
	}

	var ownRawDevices []waTypes.JID
	ownPN := internals.GetOwnID().ToNonAD()
	if !ownPN.IsEmpty() {
		if devs, ownErr := client.GetUserDevices(ctx, []waTypes.JID{ownPN}); ownErr == nil {
			ownRawDevices = append(ownRawDevices, devs...)
		}
	}
	if devs, ownErr := client.GetUserDevices(ctx, []waTypes.JID{ownLID.ToNonAD()}); ownErr == nil {
		ownRawDevices = append(ownRawDevices, devs...)
	}

	allDevices, ownDeviceIDs := mergeStatusDevices(contactRawDevices, ownRawDevices, ownLID)

	encAttrs := waBinary.Attrs{}
	msgType := "text"
	mediaType := statusMediaType(msg)
	if mediaType != "" {
		msgType = "media"
	}
	attrs := waBinary.Attrs{
		"id":   msgID,
		"type": msgType,
		"to":   to,
	}
	isRevoke := msg.ProtocolMessage != nil && msg.ProtocolMessage.GetType() == waE2E.ProtocolMessage_REVOKE
	if isRevoke {
		attrs["edit"] = string(waTypes.EditAttributeSenderRevoke)
		encAttrs["decrypt-fail"] = string(events.DecryptFailHide)
	}

	participantNodes, includeIdentity, err := internals.EncryptMessageForDevices(ctx, allDevices, msgID, skdPlaintext, nil, encAttrs)
	if err != nil {
		return whatsmeow.SendResponse{}, fmt.Errorf("encrypt SKDM for status devices: %w", err)
	}

	ownPrimary := waTypes.JID{User: ownLID.User, Device: 0, Server: waTypes.HiddenUserServer}
	ownPrimaryEncrypted := false
	for _, pn := range participantNodes {
		if j, ok := pn.Attrs["jid"].(waTypes.JID); ok && j.User == ownLID.User && j.Device == 0 {
			ownPrimaryEncrypted = true
			break
		}
	}
	if !ownPrimaryEncrypted && ownLID.Device != 0 {
		fmt.Printf("⚠️ [STATUS] primary phone %s was not encrypted in SKDM fanout — adding bare participant fallback\n", ownPrimary)
		participantNodes = append(participantNodes, waBinary.Node{
			Tag:   "to",
			Attrs: waBinary.Attrs{"jid": ownPrimary},
		})
	}

	phash := statusParticipantListHashV2(allDevices)
	attrs["phash"] = phash

	content := []waBinary.Node{{
		Tag:     "participants",
		Content: participantNodes,
	}}
	if includeIdentity {
		content = append(content, internals.MakeDeviceIdentityNode())
	}
	skMsg := waBinary.Node{
		Tag:     "enc",
		Content: ciphertext,
		Attrs:   waBinary.Attrs{"v": "2", "type": "skmsg"},
	}
	if mediaType != "" {
		skMsg.Attrs["mediatype"] = mediaType
	}
	content = append(content, skMsg)
	if internals.ShouldIncludeReportingToken(msg) && len(msg.GetMessageContextInfo().GetMessageSecret()) > 0 {
		content = append(content, internals.GetMessageReportingToken(plaintext, msg, ownLID, to, msgID))
	}
	// Attach <meta status_setting="..."/> on actual status posts (omitted on revokes/reactions per WA Web).
	if !isRevoke && msg.ReactionMessage == nil {
		content = append(content, waBinary.Node{
			Tag:   "meta",
			Attrs: waBinary.Attrs{"status_setting": statusPrivacySettingAttr(priv.Type)},
		})
	}

	node := waBinary.Node{
		Tag:     "message",
		Attrs:   attrs,
		Content: content,
	}

	_ = internals.AddRecentMessage(ctx, to, msgID, msg, nil)
	if client.Store.MsgSecrets != nil && len(msg.GetMessageContextInfo().GetMessageSecret()) > 0 {
		_ = client.Store.MsgSecrets.PutMessageSecret(ctx, to, ownLID, msgID, msg.GetMessageContextInfo().GetMessageSecret())
	}
	respChan := internals.WaitResponse(msgID)
	if _, err := internals.SendNodeAndGetData(ctx, node); err != nil {
		internals.CancelResponse(msgID, respChan)
		return whatsmeow.SendResponse{}, fmt.Errorf("send status node: %w", err)
	}

	var respNode *waBinary.Node
	select {
	case respNode = <-respChan:
	case <-time.After(60 * time.Second):
		internals.CancelResponse(msgID, respChan)
		return whatsmeow.SendResponse{}, whatsmeow.ErrMessageTimedOut
	case <-ctx.Done():
		internals.CancelResponse(msgID, respChan)
		return whatsmeow.SendResponse{}, ctx.Err()
	}

	ag := respNode.AttrGetter()
	resp := whatsmeow.SendResponse{
		ID:        msgID,
		Timestamp: ag.UnixTime("t"),
		ServerID:  waTypes.MessageServerID(ag.OptionalInt("server_id")),
	}
	if errCode := ag.Int("error"); errCode != 0 {
		fmt.Printf("⚠️ [STATUS] server error %d (privacy=%s lidRecipients=%d devices=%d ownDevices=%v ownPrimary=%v type=%s media=%s)\n",
			errCode, priv.Type, len(recipients), len(allDevices), ownDeviceIDs, ownPrimaryEncrypted, msgType, mediaType)
		return resp, fmt.Errorf("%w %d", whatsmeow.ErrServerReturnedError, errCode)
	}
	if expectedPHash := ag.OptionalString("phash"); expectedPHash != "" && expectedPHash != phash {
		fmt.Printf("⚠️ [STATUS] server returned different phash (%s != %s) for %s\n", phash, expectedPHash, msgID)
	}
	fmt.Printf("✅ [STATUS] posted %s via LID (privacy=%s setting=%s recipients=%d devices=%d ownDevices=%v ownPrimary=%v reshareable=true)\n",
		msgID, priv.Type, statusPrivacySettingAttr(priv.Type), len(recipients), len(allDevices), ownDeviceIDs, ownPrimaryEncrypted)
	return resp, nil
}

// EnsureWhatsAppStatusMP4 converts non-MP4 video files (e.g. .mov, .mkv, .webm)
// into a WhatsApp-compatible H.264/AAC MP4 at dstPath. Returns true if converted.
func EnsureWhatsAppStatusMP4(srcPath, dstPath, mimeType string) bool {
	lower := strings.ToLower(strings.TrimSpace(mimeType))
	if lower == "video/mp4" {
		return false
	}
	ffmpegPath, cleanup, err := GetFFmpegPath()
	if err != nil {
		return false
	}
	defer cleanup()
	cmd := exec.Command(ffmpegPath,
		"-y", "-i", srcPath,
		"-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart",
		dstPath,
	)
	return cmd.Run() == nil
}
