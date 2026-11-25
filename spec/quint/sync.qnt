module elport_sync {
  pure val NORMAL_TEXTS = Set("v1", "v2", "v3")
  pure val LINK_SECRET = "link_secret"
  pure val ALL_TEXTS = NORMAL_TEXTS.union(Set(LINK_SECRET, ""))

  pure val BACKTICK_HTML_SECRET = "bt_html_secret"

  pure val FORMAT_OMITTED = ""
  pure val FORMAT_MARKDOWN = "markdown"
  pure val FORMAT_HTML = "html"
  pure val ALL_FORMATS = Set(FORMAT_OMITTED, FORMAT_MARKDOWN, FORMAT_HTML)

  pure val CT_NONE = 0
  pure val CT_HTML = 1
  pure val CT_MD = 2

  pure val USER_OWNER = "owner"
  pure val USER_COLLAB = "collab"
  pure val ALL_USERS = Set(USER_OWNER, USER_COLLAB)

  pure def serverNorm(localText: str): str =
    if (localText == "v1") "v1|norm"
    else if (localText == "v2") "v2|norm"
    else if (localText == "v3") "v3|norm"
    else if (localText == "link_secret") "link_secret|norm"
    else ""

  pure def serverHtmlNorm(localText: str): str =
    if (localText == "v1") "v1|html"
    else if (localText == "v2") "v2|html"
    else if (localText == "v3") "v3|html"
    else if (localText == "link_secret") "link_secret|html"
    else ""

  pure def serverReverse(remoteText: str): str =
    if (remoteText == "v1|norm") "v1"
    else if (remoteText == "v2|norm") "v2"
    else if (remoteText == "v3|norm") "v3"
    else if (remoteText == "link_secret|norm") "link_secret"
    else ""

  pure def hasBareSecretPathForMode(body: str, ct: int): bool =
    body == LINK_SECRET
      or body == "link_secret|norm"
      or body == "link_secret|html"
      or (body == BACKTICK_HTML_SECRET and ct == CT_HTML)

  pure def hasLocalBareSecretPath(body: str): bool =
    body == LINK_SECRET

  var localBody: str
  var localFormat: str
  var hasId: bool
  var listedSecret: bool
  var currentUser: str
  var localMetaRev: int
  var sidecarsExist: bool
  var sidecarRemote: str
  var sidecarBase: str
  var sidecarListedSecret: bool

  var hasBase: bool
  var localBase: str
  var remoteBase: str
  var savedContentType: int
  var metaBaseRev: int
  var pendingCreate: bool
  var bodySent: bool
  var hasPendingRemote: bool
  var pendingRemote: str
  var rememberedSecret: bool
  var hasPendingPublish: bool
  var pendingPublishTargetCT: int
  var pendingPublishUser: str
  var pendingPublishPreBody: str
  var pendingPublishPreCT: int

  var remoteExists: bool
  var remoteBody: str
  var remoteLocked: bool
  var remoteIsHtml: bool
  var remoteOwner: str
  var remoteMetaRev: int
  var secretUploaded: bool

  var lastAction: str
  var lastArg: str
  var lastOutcome: str
  var prevLocalBody: str
  var prevLocalFormat: str
  var prevCurrentUser: str
  var prevHasBase: bool
  var prevSavedContentType: int
  var prevRemoteBase: str
  var prevHasPendingPublish: bool
  var prevPendingPublishTargetCT: int
  var prevPendingPublishUser: str
  var prevPendingUnchanged: bool
  var prevRemoteBody: str
  var prevRemoteLocked: bool
  var prevRemoteIsHtml: bool
  var prevRemoteOwner: str
  var prevSecretUploaded: bool
  var prevListedSecret: bool
  var prevSecretRemoteLinked: bool
  var prevSidecarsExist: bool
  var prevSidecarRemote: str
  var prevSidecarBase: str
  var prevHasConfirm: bool
  var prevConfirmBody: str
  var postReGetRaceOccurred: bool

  pure def remoteCTOf(hasRemote: bool, isHtml: bool): int =
    if (not(hasRemote)) CT_NONE else if (isHtml) CT_HTML else CT_MD

  pure def savedBaseCT(pSavedCT: int): int =
    if (pSavedCT == CT_NONE) CT_MD else pSavedCT

  pure def isConfirmedMdBase(pHasBase: bool, pSavedCT: int, pPendingCreate: bool): bool =
    pHasBase and not(pPendingCreate) and (pSavedCT == CT_MD or pSavedCT == CT_NONE)

  pure def isResuming(
    pHasBase: bool,
    pPendingCreate: bool,
    pBodySent: bool,
    pHasPendingRemote: bool
  ): bool =
    pHasBase and pPendingCreate and pBodySent and not(pHasPendingRemote)

  pure def isSecretRemoteLinked(
    pRemoteBody: str,
    pRemoteCT: int,
    pRemoteBase: str,
    pSavedCT: int,
    pPendingRemote: str,
    pHasPendingPublish: bool,
    pPendingPreBody: str,
    pPendingPreCT: int,
    pRememberedSecret: bool
  ): bool =
    pRememberedSecret
      or hasBareSecretPathForMode(pRemoteBody, pRemoteCT)
      or hasBareSecretPathForMode(pRemoteBase, savedBaseCT(pSavedCT))
      or hasBareSecretPathForMode(pPendingRemote, CT_MD)
      or (pHasPendingPublish and hasBareSecretPathForMode(pPendingPreBody, pPendingPreCT))

  pure def sidecarWouldClobber(
    pSidecarsExist: bool,
    pSidecarRemote: str,
    pSidecarBase: str,
    pSidecarListed: bool,
    wantRemote: str,
    wantBase: str,
    wantListed: bool
  ): bool =
    pSidecarsExist and (
      pSidecarRemote != wantRemote
        or pSidecarBase != wantBase
        or pSidecarListed != wantListed
    )

  pure def evalPushDecision(
    pLocalFormat: str,
    pHasId: bool,
    pCurrentUser: str,
    pHasBase: bool,
    pRemoteBase: str,
    pSavedCT: int,
    pPendingCreate: bool,
    pHasPendingPublish: bool,
    pPendingTargetCT: int,
    pPendingUser: str,
    pPendingPreBody: str,
    pPendingPreCT: int,
    pRemoteExists: bool,
    pRemoteBody: str,
    pRemoteLocked: bool,
    pRemoteIsHtml: bool,
    pRemoteOwner: str,
    pHasConfirm: bool,
    pConfirmBody: str
  ): str = {
    val wantHtml = pLocalFormat == FORMAT_HTML
    val explicitMd = pLocalFormat == FORMAT_MARKDOWN
    val curCT = remoteCTOf(pRemoteExists, pRemoteIsHtml)
    val isRegHtml = pHasBase and pSavedCT == CT_HTML
    val isConfMd = isConfirmedMdBase(pHasBase, pSavedCT, pPendingCreate)
    val confirmMatches = pHasConfirm and pRemoteExists and pConfirmBody == pRemoteBody
    val isOwner = pRemoteExists and pRemoteOwner == pCurrentUser
    val pendingUnchanged =
      pHasPendingPublish
        and pRemoteExists
        and pRemoteBody == pPendingPreBody
        and curCT == pPendingPreCT
    val formatMatchesPending =
      (wantHtml and pPendingTargetCT == CT_HTML)
        or (explicitMd and pPendingTargetCT == CT_MD)

    if (pRemoteExists and pRemoteLocked)
      "refused_locked"

    else if (pHasPendingPublish and not(pendingUnchanged))
      if (pCurrentUser == pPendingUser and confirmMatches and formatMatchesPending)
        (if (wantHtml) "ok_html" else "ok_md_revert")
      else
        "refused_pending_publish"

    else if (isRegHtml and pRemoteExists and not(pRemoteIsHtml))
      if (wantHtml and confirmMatches) "ok_html"
      else if (explicitMd and confirmMatches) "ok_md_revert"
      else "refused_mode_mismatch"

    else if (wantHtml)
      if ((not(pHasId) and not(pRemoteExists))
          or (pRemoteExists and pRemoteBody == "" and not(pHasBase and pRemoteBase != "") and not(isRegHtml)))
        "ok_html"
      else if (pRemoteExists and not(pRemoteIsHtml))

        if (not(isConfMd)) "refused_no_base"
        else if (pRemoteBody != pRemoteBase) "conflict_html"
        else "ok_html"
      else if (isRegHtml)

        if (pRemoteBody == pRemoteBase or confirmMatches) "ok_html"
        else "conflict_html"
      else

        if (isOwner and confirmMatches) "ok_html_adopt"
        else "refused_html"

    else if (pRemoteExists and pRemoteIsHtml and (pRemoteBody != "" or isRegHtml))
      if (explicitMd and isRegHtml)

        if (pRemoteBody == pRemoteBase or confirmMatches) "ok_md_revert"
        else "conflict_html"
      else

        "refused_html"

    else
      "markdown_normal"
  }

  pure def isHtmlModeAllowedDecision(dec: str): bool =
    dec == "ok_html" or dec == "ok_html_adopt" or dec == "ok_md_revert"

  pure def needsPendingPublish(
    dec: str,
    pHasBase: bool,
    pSavedCT: int,
    pRemoteExists: bool,
    pRemoteIsHtml: bool
  ): bool =
    (dec == "ok_md_revert" and pRemoteExists and pRemoteIsHtml)
      or (dec == "ok_html" and (not(pRemoteExists) or not(pRemoteIsHtml) or not(pHasBase and pSavedCT == CT_HTML)))

  pure def decisionTargetCT(dec: str): int =
    if (dec == "ok_md_revert") CT_MD else CT_HTML

  action init = all {
    localBody' = "v1",
    localFormat' = FORMAT_OMITTED,
    hasId' = false,
    listedSecret' = false,
    currentUser' = USER_OWNER,
    localMetaRev' = 0,
    sidecarsExist' = false,
    sidecarRemote' = "",
    sidecarBase' = "",
    sidecarListedSecret' = false,

    hasBase' = false,
    localBase' = "",
    remoteBase' = "",
    savedContentType' = CT_NONE,
    metaBaseRev' = 0,
    pendingCreate' = false,
    bodySent' = false,
    hasPendingRemote' = false,
    pendingRemote' = "",
    rememberedSecret' = false,
    hasPendingPublish' = false,
    pendingPublishTargetCT' = CT_NONE,
    pendingPublishUser' = "",
    pendingPublishPreBody' = "",
    pendingPublishPreCT' = CT_NONE,

    remoteExists' = false,
    remoteBody' = "",
    remoteLocked' = false,
    remoteIsHtml' = false,
    remoteOwner' = USER_OWNER,
    remoteMetaRev' = 0,
    secretUploaded' = false,

    lastAction' = "init",
    lastArg' = "",
    lastOutcome' = "ok",
    prevLocalBody' = "v1",
    prevLocalFormat' = FORMAT_OMITTED,
    prevCurrentUser' = USER_OWNER,
    prevHasBase' = false,
    prevSavedContentType' = CT_NONE,
    prevRemoteBase' = "",
    prevHasPendingPublish' = false,
    prevPendingPublishTargetCT' = CT_NONE,
    prevPendingPublishUser' = "",
    prevPendingUnchanged' = false,
    prevRemoteBody' = "",
    prevRemoteLocked' = false,
    prevRemoteIsHtml' = false,
    prevRemoteOwner' = USER_OWNER,
    prevSecretUploaded' = false,
    prevListedSecret' = false,
    prevSecretRemoteLinked' = false,
    prevSidecarsExist' = false,
    prevSidecarRemote' = "",
    prevSidecarBase' = "",
    prevHasConfirm' = false,
    prevConfirmBody' = "",
    postReGetRaceOccurred' = false,
  }

  action recordGhostFull(
    act: str,
    arg: str,
    outcome: str,
    hasConfirm: bool,
    confirmBody: str,
    raceOccurred: bool
  ): bool = all {
    lastAction' = act,
    lastArg' = arg,
    lastOutcome' = outcome,
    prevLocalBody' = localBody,
    prevLocalFormat' = localFormat,
    prevCurrentUser' = currentUser,
    prevHasBase' = hasBase,
    prevSavedContentType' = savedContentType,
    prevRemoteBase' = remoteBase,
    prevHasPendingPublish' = hasPendingPublish,
    prevPendingPublishTargetCT' = pendingPublishTargetCT,
    prevPendingPublishUser' = pendingPublishUser,
    prevPendingUnchanged' = (
      hasPendingPublish
        and remoteExists
        and remoteBody == pendingPublishPreBody
        and remoteCTOf(remoteExists, remoteIsHtml) == pendingPublishPreCT
    ),
    prevRemoteBody' = remoteBody,
    prevRemoteLocked' = remoteLocked,
    prevRemoteIsHtml' = remoteIsHtml,
    prevRemoteOwner' = remoteOwner,
    prevSecretUploaded' = secretUploaded,
    prevListedSecret' = listedSecret,
    prevSecretRemoteLinked' = isSecretRemoteLinked(
      remoteBody,
      remoteCTOf(remoteExists, remoteIsHtml),
      remoteBase,
      savedContentType,
      pendingRemote,
      hasPendingPublish,
      pendingPublishPreBody,
      pendingPublishPreCT,
      rememberedSecret
    ),
    prevSidecarsExist' = sidecarsExist,
    prevSidecarRemote' = sidecarRemote,
    prevSidecarBase' = sidecarBase,
    prevHasConfirm' = hasConfirm,
    prevConfirmBody' = confirmBody,
    postReGetRaceOccurred' = raceOccurred,
  }

  action recordGhost(act: str, arg: str, outcome: str): bool =
    recordGhostFull(act, arg, outcome, false, "", false)

  action unchangedSidecars: bool = all {
    sidecarsExist' = sidecarsExist,
    sidecarRemote' = sidecarRemote,
    sidecarBase' = sidecarBase,
    sidecarListedSecret' = sidecarListedSecret,
  }

  action unchangedPendingPublish: bool = all {
    hasPendingPublish' = hasPendingPublish,
    pendingPublishTargetCT' = pendingPublishTargetCT,
    pendingPublishUser' = pendingPublishUser,
    pendingPublishPreBody' = pendingPublishPreBody,
    pendingPublishPreCT' = pendingPublishPreCT,
  }

  action clearPendingPublish: bool = all {
    hasPendingPublish' = false,
    pendingPublishTargetCT' = CT_NONE,
    pendingPublishUser' = "",
    pendingPublishPreBody' = "",
    pendingPublishPreCT' = CT_NONE,
  }

  action unchangedBaseState: bool = all {
    hasBase' = hasBase,
    localBase' = localBase,
    remoteBase' = remoteBase,
    savedContentType' = savedContentType,
    metaBaseRev' = metaBaseRev,
    pendingCreate' = pendingCreate,
    bodySent' = bodySent,
    hasPendingRemote' = hasPendingRemote,
    pendingRemote' = pendingRemote,
    rememberedSecret' = rememberedSecret,
    unchangedPendingPublish,
  }

  action unchangedRemoteState: bool = all {
    remoteExists' = remoteExists,
    remoteBody' = remoteBody,
    remoteLocked' = remoteLocked,
    remoteIsHtml' = remoteIsHtml,
    remoteOwner' = remoteOwner,
    remoteMetaRev' = remoteMetaRev,
    secretUploaded' = secretUploaded,
  }

  action unchangedLocalMeta: bool = all {
    localFormat' = localFormat,
    currentUser' = currentUser,
    localMetaRev' = localMetaRev,
  }

  action unchangedState: bool = all {
    localBody' = localBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    unchangedLocalMeta,
    unchangedSidecars,
    unchangedBaseState,
    unchangedRemoteState,
  }

  action localEdit(newBody: str): bool = all {
    newBody != localBody,
    localBody' = newBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    unchangedLocalMeta,
    unchangedSidecars,
    unchangedBaseState,
    unchangedRemoteState,
    recordGhost("local_edit", newBody, "ok"),
  }

  action setLocalFormat(newFormat: str): bool = all {
    newFormat != localFormat,
    localFormat' = newFormat,
    localBody' = localBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    currentUser' = currentUser,
    localMetaRev' = localMetaRev,
    unchangedSidecars,
    unchangedBaseState,
    unchangedRemoteState,
    recordGhost("set_format", newFormat, "ok"),
  }

  action setListedSecret(listed: bool): bool = all {
    listed != listedSecret,
    listedSecret' = listed,
    localBody' = localBody,
    hasId' = hasId,
    unchangedLocalMeta,
    unchangedSidecars,
    unchangedBaseState,
    unchangedRemoteState,
    recordGhost(if (listed) "list_secret" else "unlist_secret", "", "ok"),
  }

  action stripLegacyContentType: bool = all {
    hasBase,
    not(pendingCreate),
    savedContentType == CT_MD,
    not(hasPendingPublish),
    localBody' = localBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    unchangedLocalMeta,
    unchangedSidecars,
    hasBase' = hasBase,
    localBase' = localBase,
    remoteBase' = remoteBase,
    savedContentType' = CT_NONE,
    metaBaseRev' = metaBaseRev,
    pendingCreate' = pendingCreate,
    bodySent' = bodySent,
    hasPendingRemote' = hasPendingRemote,
    pendingRemote' = pendingRemote,
    rememberedSecret' = rememberedSecret,
    unchangedPendingPublish,
    unchangedRemoteState,
    recordGhost("legacy_stage1_state", "", "ok"),
  }

  action switchToSecondMachine(user: str): bool = all {
    hasId,
    remoteExists,
    localBody' = localBody,
    localFormat' = localFormat,
    hasId' = hasId,
    listedSecret' = listedSecret,
    currentUser' = user,
    localMetaRev' = localMetaRev,
    sidecarsExist' = false,
    sidecarRemote' = "",
    sidecarBase' = "",
    sidecarListedSecret' = false,
    hasBase' = false,
    localBase' = "",
    remoteBase' = "",
    savedContentType' = CT_NONE,
    metaBaseRev' = 0,
    pendingCreate' = false,
    bodySent' = false,
    hasPendingRemote' = false,
    pendingRemote' = "",
    rememberedSecret' = false,
    clearPendingPublish,
    unchangedRemoteState,
    recordGhost("switch_machine", user, "ok"),
  }

  action webEdit(newBody: str): bool = all {
    remoteExists,
    not(remoteLocked),
    newBody != remoteBody,
    remoteBody' = newBody,
    localBody' = localBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    unchangedLocalMeta,
    unchangedSidecars,
    unchangedBaseState,
    remoteExists' = remoteExists,
    remoteLocked' = remoteLocked,
    remoteIsHtml' = remoteIsHtml,
    remoteOwner' = remoteOwner,
    remoteMetaRev' = remoteMetaRev,
    secretUploaded' = secretUploaded,
    recordGhost("web_edit", newBody, "ok"),
  }

  action webEditMeta: bool = all {
    remoteExists,
    remoteMetaRev < 2,
    remoteMetaRev' = remoteMetaRev + 1,
    localBody' = localBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    unchangedLocalMeta,
    unchangedSidecars,
    unchangedBaseState,
    remoteExists' = remoteExists,
    remoteBody' = remoteBody,
    remoteLocked' = remoteLocked,
    remoteIsHtml' = remoteIsHtml,
    remoteOwner' = remoteOwner,
    secretUploaded' = secretUploaded,
    recordGhost("web_edit_meta", "", "ok"),
  }

  action webSetLock(locked: bool): bool = all {
    remoteExists,
    locked != remoteLocked,
    remoteLocked' = locked,
    localBody' = localBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    unchangedLocalMeta,
    unchangedSidecars,
    unchangedBaseState,
    remoteExists' = remoteExists,
    remoteBody' = remoteBody,
    remoteIsHtml' = remoteIsHtml,
    remoteOwner' = remoteOwner,
    remoteMetaRev' = remoteMetaRev,
    secretUploaded' = secretUploaded,
    recordGhost(if (locked) "web_lock" else "web_unlock", "", "ok"),
  }

  action webSetHtml(htmlMode: bool): bool = all {
    remoteExists,
    htmlMode != remoteIsHtml,
    remoteIsHtml' = htmlMode,
    localBody' = localBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    unchangedLocalMeta,
    unchangedSidecars,
    unchangedBaseState,
    remoteExists' = remoteExists,
    remoteBody' = remoteBody,
    remoteLocked' = remoteLocked,
    remoteOwner' = remoteOwner,
    remoteMetaRev' = remoteMetaRev,
    secretUploaded' = secretUploaded,
    recordGhost(if (htmlMode) "web_html_on" else "web_html_off", "", "ok"),
  }

  action webSetOwner(newOwner: str): bool = all {
    remoteExists,
    newOwner != remoteOwner,
    remoteOwner' = newOwner,
    localBody' = localBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    unchangedLocalMeta,
    unchangedSidecars,
    unchangedBaseState,
    remoteExists' = remoteExists,
    remoteBody' = remoteBody,
    remoteLocked' = remoteLocked,
    remoteIsHtml' = remoteIsHtml,
    remoteMetaRev' = remoteMetaRev,
    secretUploaded' = secretUploaded,
    recordGhost("web_set_owner", newOwner, "ok"),
  }

  action pushCreateInterrupted(patchReachedServer: bool): bool = {
    val stored = if (patchReachedServer) serverNorm(localBody) else ""
    val uploadedNow =
      patchReachedServer and (listedSecret or hasLocalBareSecretPath(localBody))
    all {
      not(hasId),
      not(remoteExists),
      localFormat != FORMAT_HTML,
      localBody != "",
      hasId' = true,
      remoteExists' = true,
      remoteBody' = stored,
      remoteOwner' = currentUser,
      remoteMetaRev' = remoteMetaRev,
      hasBase' = true,
      localBase' = "",
      remoteBase' = "",
      savedContentType' = CT_NONE,
      metaBaseRev' = 0,
      pendingCreate' = true,
      bodySent' = patchReachedServer,
      hasPendingRemote' = false,
      pendingRemote' = "",
      rememberedSecret' = rememberedSecret,
      clearPendingPublish,
      localBody' = localBody,
      listedSecret' = listedSecret,
      unchangedLocalMeta,
      unchangedSidecars,
      remoteLocked' = false,
      remoteIsHtml' = false,
      secretUploaded' = (secretUploaded or uploadedNow),
      recordGhost(
        if (patchReachedServer) "push_create_lost_response" else "push_create_fail_before_patch",
        "",
        "interrupted"
      ),
    }
  }

  action pushPublishInterrupted(
    hasConfirm: bool,
    confirmBody: str,
    patchReachedServer: bool
  ): bool = {
    val dec = evalPushDecision(
      localFormat, hasId, currentUser,
      hasBase, remoteBase, savedContentType, pendingCreate,
      hasPendingPublish, pendingPublishTargetCT, pendingPublishUser,
      pendingPublishPreBody, pendingPublishPreCT,
      remoteExists, remoteBody, remoteLocked, remoteIsHtml, remoteOwner,
      hasConfirm, confirmBody
    )
    val writePending =
      needsPendingPublish(dec, hasBase, savedContentType, remoteExists, remoteIsHtml)
    val targetCT = decisionTargetCT(dec)
    val curCT = remoteCTOf(remoteExists, remoteIsHtml)
    val secretLinked = isSecretRemoteLinked(
      remoteBody, curCT, remoteBase, savedContentType, pendingRemote,
      hasPendingPublish, pendingPublishPreBody, pendingPublishPreCT, rememberedSecret
    )
    val shouldUploadSecret =
      listedSecret or (hasLocalBareSecretPath(localBody) and not(secretLinked))
    val nextRemembered =
      rememberedSecret
        or hasBareSecretPathForMode(remoteBase, savedBaseCT(savedContentType))
        or hasBareSecretPathForMode(pendingRemote, CT_MD)
        or hasBareSecretPathForMode(remoteBody, curCT)
    val nextBody =
      if (not(patchReachedServer)) remoteBody
      else if (targetCT == CT_HTML) serverHtmlNorm(localBody)
      else serverNorm(localBody)
    val nextIsHtml =
      if (not(patchReachedServer)) (if (not(remoteExists)) true else remoteIsHtml)
      else (targetCT == CT_HTML)
    val nextOwner = if (not(remoteExists)) currentUser else remoteOwner
    all {
      not(hasPendingPublish),
      isHtmlModeAllowedDecision(dec),
      localBody' = localBody,
      hasId' = true,
      listedSecret' = listedSecret,
      unchangedLocalMeta,
      unchangedSidecars,

      hasBase' = hasBase,
      localBase' = localBase,
      remoteBase' = remoteBase,
      savedContentType' = savedContentType,
      metaBaseRev' = metaBaseRev,
      pendingCreate' = pendingCreate,
      bodySent' = bodySent,
      hasPendingRemote' = hasPendingRemote,
      pendingRemote' = pendingRemote,
      rememberedSecret' = nextRemembered,
      hasPendingPublish' = writePending,
      pendingPublishTargetCT' = if (writePending) targetCT else CT_NONE,
      pendingPublishUser' = if (writePending) currentUser else "",
      pendingPublishPreBody' = if (writePending) remoteBody else "",
      pendingPublishPreCT' = if (writePending) (if (not(remoteExists)) CT_HTML else curCT) else CT_NONE,
      remoteExists' = true,
      remoteBody' = nextBody,
      remoteLocked' = remoteLocked,
      remoteIsHtml' = nextIsHtml,
      remoteOwner' = nextOwner,
      remoteMetaRev' = remoteMetaRev,
      secretUploaded' = (secretUploaded or shouldUploadSecret),
      recordGhostFull(
        if (patchReachedServer) "push_publish_lost_response" else "push_publish_fail_before_patch",
        if (targetCT == CT_HTML) "html" else "markdown",
        "interrupted",
        hasConfirm,
        confirmBody,
        false
      ),
    }
  }

  action commitPush(
    actName: str,
    arg: str,
    targetCT: int,
    hasConfirm: bool,
    confirmBody: str
  ): bool = {
    val curCT = remoteCTOf(remoteExists, remoteIsHtml)
    val secretLinked = isSecretRemoteLinked(
      remoteBody, curCT, remoteBase, savedContentType, pendingRemote,
      hasPendingPublish, pendingPublishPreBody, pendingPublishPreCT, rememberedSecret
    )
    val shouldUploadSecret =
      listedSecret or (hasLocalBareSecretPath(localBody) and not(secretLinked))
    val nextRemoteBody =
      if (targetCT == CT_HTML) serverHtmlNorm(localBody) else serverNorm(localBody)
    val nextRemembered =
      rememberedSecret
        or hasBareSecretPathForMode(remoteBase, savedBaseCT(savedContentType))
        or hasBareSecretPathForMode(pendingRemote, CT_MD)
        or hasBareSecretPathForMode(remoteBody, curCT)
        or (hasPendingPublish and hasBareSecretPathForMode(pendingPublishPreBody, pendingPublishPreCT))
    val nextUploaded = secretUploaded or shouldUploadSecret
    val nextOwner = if (not(remoteExists)) currentUser else remoteOwner
    all {
      localBody' = localBody,
      hasId' = true,
      listedSecret' = listedSecret,
      unchangedLocalMeta,
      unchangedSidecars,
      hasBase' = true,
      localBase' = localBody,
      remoteBase' = nextRemoteBody,
      savedContentType' = targetCT,
      metaBaseRev' = remoteMetaRev,
      pendingCreate' = false,
      bodySent' = false,
      hasPendingRemote' = false,
      pendingRemote' = "",
      rememberedSecret' = nextRemembered,
      clearPendingPublish,
      remoteExists' = true,
      remoteBody' = nextRemoteBody,
      remoteLocked' = remoteLocked,
      remoteIsHtml' = (targetCT == CT_HTML),
      remoteOwner' = nextOwner,
      remoteMetaRev' = remoteMetaRev,
      secretUploaded' = nextUploaded,
      recordGhostFull(actName, arg, "pushed", hasConfirm, confirmBody, false),
    }
  }

  action pushWith(force: bool, hasConfirm: bool, confirmBody: str): bool = {
    val actName =
      if (hasConfirm) "push_confirm"
      else if (force) "push_force"
      else "push"
    val arg = if (hasConfirm) confirmBody else ""
    val dec = evalPushDecision(
      localFormat, hasId, currentUser,
      hasBase, remoteBase, savedContentType, pendingCreate,
      hasPendingPublish, pendingPublishTargetCT, pendingPublishUser,
      pendingPublishPreBody, pendingPublishPreCT,
      remoteExists, remoteBody, remoteLocked, remoteIsHtml, remoteOwner,
      hasConfirm, confirmBody
    )
    val resuming =
      isResuming(hasBase, pendingCreate, bodySent, hasPendingRemote)
        and localFormat != FORMAT_HTML
        and not(hasBase and savedContentType == CT_HTML)
        and not(hasPendingPublish)

    if (isHtmlModeAllowedDecision(dec)) {
      commitPush(actName, arg, decisionTargetCT(dec), hasConfirm, confirmBody)
    } else if (dec != "markdown_normal") all {
      unchangedState,
      recordGhostFull(actName, arg, dec, hasConfirm, confirmBody, false),
    }

    else if (hasId and not(hasBase) and not(force)) all {
      unchangedState,
      recordGhostFull(actName, arg, "refused_no_base", hasConfirm, confirmBody, false),
    } else if (hasId and hasBase and not(resuming) and remoteBody != remoteBase and not(force)) {
      if (sidecarWouldClobber(sidecarsExist, sidecarRemote, sidecarBase, sidecarListedSecret, remoteBody, localBase, listedSecret)) all {
        unchangedState,
        recordGhostFull(actName, arg, "refused_sidecar", hasConfirm, confirmBody, false),
      } else all {
        localBody' = localBody,
        hasId' = hasId,
        listedSecret' = listedSecret,
        unchangedLocalMeta,
        sidecarsExist' = true,
        sidecarRemote' = remoteBody,
        sidecarBase' = localBase,
        sidecarListedSecret' = listedSecret,
        hasBase' = hasBase,
        localBase' = localBase,
        remoteBase' = remoteBase,
        savedContentType' = savedContentType,
        metaBaseRev' = metaBaseRev,
        pendingCreate' = pendingCreate,
        bodySent' = bodySent,
        hasPendingRemote' = true,
        pendingRemote' = remoteBody,
        rememberedSecret' = (rememberedSecret or hasBareSecretPathForMode(pendingRemote, CT_MD)),
        unchangedPendingPublish,
        unchangedRemoteState,
        recordGhostFull(actName, arg, "conflict", hasConfirm, confirmBody, false),
      }
    } else {
      commitPush(actName, arg, CT_MD, hasConfirm, confirmBody)
    }
  }

  action push(force: bool): bool =
    pushWith(force, false, "")

  action pushPrePatchReGetBlocked(
    hasConfirm: bool,
    confirmBody: str,
    concurrentWebBody: str,
    concurrentIsHtml: bool,
    concurrentLock: bool,
    concurrentOwner: str
  ): bool = {
    val dec = evalPushDecision(
      localFormat, hasId, currentUser,
      hasBase, remoteBase, savedContentType, pendingCreate,
      hasPendingPublish, pendingPublishTargetCT, pendingPublishUser,
      pendingPublishPreBody, pendingPublishPreCT,
      remoteExists, remoteBody, remoteLocked, remoteIsHtml, remoteOwner,
      hasConfirm, confirmBody
    )
    val needsOwner = dec == "ok_html_adopt"
    val curCT = remoteCTOf(remoteExists, remoteIsHtml)
    val secretLinked = isSecretRemoteLinked(
      remoteBody, curCT, remoteBase, savedContentType, pendingRemote,
      hasPendingPublish, pendingPublishPreBody, pendingPublishPreCT, rememberedSecret
    )
    val shouldUploadSecret =
      listedSecret or (hasLocalBareSecretPath(localBody) and not(secretLinked))
    val ownerBroken = needsOwner and concurrentOwner != currentUser
    val bodyOrModeChanged =
      concurrentWebBody != remoteBody or concurrentIsHtml != remoteIsHtml
    val blocked = concurrentLock or ownerBroken or bodyOrModeChanged
    val outcome =
      if (concurrentLock) "refused_locked"
      else if (ownerBroken) "refused_html"
      else if (concurrentIsHtml != remoteIsHtml) "refused_mode_mismatch"
      else "conflict_html"
    all {
      hasId,
      remoteExists,
      isHtmlModeAllowedDecision(dec),
      blocked,
      localBody' = localBody,
      hasId' = hasId,
      listedSecret' = listedSecret,
      unchangedLocalMeta,
      unchangedSidecars,
      unchangedBaseState,
      remoteExists' = remoteExists,
      remoteBody' = concurrentWebBody,
      remoteLocked' = concurrentLock,
      remoteIsHtml' = concurrentIsHtml,
      remoteOwner' = concurrentOwner,
      remoteMetaRev' = remoteMetaRev,
      secretUploaded' = (secretUploaded or shouldUploadSecret),
      recordGhostFull(
        "push_pre_patch_reget_blocked",
        concurrentWebBody,
        outcome,
        hasConfirm,
        confirmBody,
        false
      ),
    }
  }

  action pushPostReGetRace(
    hasConfirm: bool,
    confirmBody: str,
    concurrentWebBody: str,
    concurrentLock: bool
  ): bool = {
    val dec = evalPushDecision(
      localFormat, hasId, currentUser,
      hasBase, remoteBase, savedContentType, pendingCreate,
      hasPendingPublish, pendingPublishTargetCT, pendingPublishUser,
      pendingPublishPreBody, pendingPublishPreCT,
      remoteExists, remoteBody, remoteLocked, remoteIsHtml, remoteOwner,
      hasConfirm, confirmBody
    )
    val targetCT = decisionTargetCT(dec)
    val curCT = remoteCTOf(remoteExists, remoteIsHtml)
    val secretLinked = isSecretRemoteLinked(
      remoteBody, curCT, remoteBase, savedContentType, pendingRemote,
      hasPendingPublish, pendingPublishPreBody, pendingPublishPreCT, rememberedSecret
    )
    val shouldUploadSecret =
      listedSecret or (hasLocalBareSecretPath(localBody) and not(secretLinked))
    val nextRemoteBody =
      if (targetCT == CT_HTML) serverHtmlNorm(localBody) else serverNorm(localBody)
    val nextRemembered =
      rememberedSecret
        or hasBareSecretPathForMode(remoteBase, savedBaseCT(savedContentType))
        or hasBareSecretPathForMode(pendingRemote, CT_MD)
        or hasBareSecretPathForMode(remoteBody, curCT)
        or (hasPendingPublish and hasBareSecretPathForMode(pendingPublishPreBody, pendingPublishPreCT))
    all {
      hasId,
      remoteExists,
      isHtmlModeAllowedDecision(dec),
      (concurrentWebBody != remoteBody or concurrentLock),
      localBody' = localBody,
      hasId' = hasId,
      listedSecret' = listedSecret,
      unchangedLocalMeta,
      unchangedSidecars,
      hasBase' = true,
      localBase' = localBody,
      remoteBase' = nextRemoteBody,
      savedContentType' = targetCT,
      metaBaseRev' = remoteMetaRev,
      pendingCreate' = false,
      bodySent' = false,
      hasPendingRemote' = false,
      pendingRemote' = "",
      rememberedSecret' = nextRemembered,
      clearPendingPublish,
      remoteExists' = true,
      remoteBody' = nextRemoteBody,
      remoteLocked' = concurrentLock,
      remoteIsHtml' = (targetCT == CT_HTML),
      remoteOwner' = remoteOwner,
      remoteMetaRev' = remoteMetaRev,
      secretUploaded' = (secretUploaded or shouldUploadSecret),
      recordGhostFull("push_post_reget_race", concurrentWebBody, "pushed_raced", hasConfirm, confirmBody, true),
    }
  }

  action pushMidRetryConflict(concurrentWebBody: str): bool = all {
    hasId,
    remoteExists,
    not(remoteLocked),
    not(remoteIsHtml and remoteBody != ""),
    localFormat != FORMAT_HTML,
    not(hasBase and savedContentType == CT_HTML),
    not(hasPendingPublish),
    isResuming(hasBase, pendingCreate, bodySent, hasPendingRemote),
    concurrentWebBody != remoteBody,
    not(sidecarWouldClobber(sidecarsExist, sidecarRemote, sidecarBase, sidecarListedSecret, concurrentWebBody, localBase, listedSecret)),
    localBody' = localBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    unchangedLocalMeta,
    sidecarsExist' = true,
    sidecarRemote' = concurrentWebBody,
    sidecarBase' = localBase,
    sidecarListedSecret' = listedSecret,
    hasBase' = hasBase,
    localBase' = localBase,
    remoteBase' = remoteBase,
    savedContentType' = savedContentType,
    metaBaseRev' = metaBaseRev,
    pendingCreate' = pendingCreate,
    bodySent' = bodySent,
    hasPendingRemote' = true,
    pendingRemote' = concurrentWebBody,
    rememberedSecret' = (rememberedSecret or hasBareSecretPathForMode(pendingRemote, CT_MD)),
    unchangedPendingPublish,
    remoteExists' = remoteExists,
    remoteBody' = concurrentWebBody,
    remoteLocked' = remoteLocked,
    remoteIsHtml' = remoteIsHtml,
    remoteOwner' = remoteOwner,
    remoteMetaRev' = remoteMetaRev,
    secretUploaded' = secretUploaded,
    recordGhost("push_mid_retry_conflict", concurrentWebBody, "conflict"),
  }

  action pull: bool = {
    val isRegHtml = hasBase and savedContentType == CT_HTML
    all {
      hasId,
      remoteExists,
      if (hasPendingPublish) all {
        unchangedState,
        recordGhost("pull", "", "refused_pending_publish"),
      } else if (isRegHtml and not(remoteIsHtml)) all {

        unchangedState,
        recordGhost("pull", "", "refused_mode_mismatch"),
      } else if (isRegHtml and remoteIsHtml) all {

        localBody' = localBody,
        localFormat' = localFormat,
        hasId' = hasId,
        listedSecret' = listedSecret,
        currentUser' = currentUser,
        localMetaRev' = remoteMetaRev,
        unchangedSidecars,
        hasBase' = hasBase,
        localBase' = localBase,
        remoteBase' = remoteBase,
        savedContentType' = savedContentType,
        metaBaseRev' = remoteMetaRev,
        pendingCreate' = pendingCreate,
        bodySent' = bodySent,
        hasPendingRemote' = hasPendingRemote,
        pendingRemote' = pendingRemote,
        rememberedSecret' = rememberedSecret,
        unchangedPendingPublish,
        unchangedRemoteState,
        recordGhost("pull", "", "pulled_meta"),
      } else if (remoteIsHtml and remoteBody != "") all {
        unchangedState,
        recordGhost("pull", "", "refused_html"),
      } else if (not(hasBase) and localBody != "" and localBody != serverReverse(remoteBody)) all {

        unchangedState,
        recordGhost("pull", "", "no_base_conflict"),
      } else if (hasBase and localBody != localBase and remoteBody == remoteBase) all {
        localBody' = localBody,
        localFormat' = localFormat,
        hasId' = hasId,
        listedSecret' = listedSecret,
        currentUser' = currentUser,
        localMetaRev' = remoteMetaRev,
        unchangedSidecars,
        hasBase' = hasBase,
        localBase' = localBase,
        remoteBase' = remoteBase,
        savedContentType' = savedContentType,
        metaBaseRev' = remoteMetaRev,
        pendingCreate' = pendingCreate,
        bodySent' = bodySent,
        hasPendingRemote' = hasPendingRemote,
        pendingRemote' = pendingRemote,
        rememberedSecret' = rememberedSecret,
        unchangedPendingPublish,
        unchangedRemoteState,
        recordGhost("pull", "", "up_to_date"),
      } else if (hasBase and localBody != localBase and remoteBody != remoteBase) {
        if (sidecarWouldClobber(sidecarsExist, sidecarRemote, sidecarBase, sidecarListedSecret, remoteBody, localBase, listedSecret)) all {
          unchangedState,
          recordGhost("pull", "", "refused_sidecar"),
        } else all {
          localBody' = localBody,
          hasId' = hasId,
          listedSecret' = listedSecret,
          unchangedLocalMeta,
          sidecarsExist' = true,
          sidecarRemote' = remoteBody,
          sidecarBase' = localBase,
          sidecarListedSecret' = listedSecret,
          hasBase' = hasBase,
          localBase' = localBase,
          remoteBase' = remoteBase,
          savedContentType' = savedContentType,
          metaBaseRev' = metaBaseRev,
          pendingCreate' = pendingCreate,
          bodySent' = bodySent,
          hasPendingRemote' = true,
          pendingRemote' = remoteBody,
          rememberedSecret' = (rememberedSecret or hasBareSecretPathForMode(pendingRemote, CT_MD)),
          unchangedPendingPublish,
          unchangedRemoteState,
          recordGhost("pull", "", "conflict"),
        }
      } else {
        val rev = serverReverse(remoteBody)
        val nextRemembered =
          rememberedSecret
            or hasBareSecretPathForMode(remoteBase, savedBaseCT(savedContentType))
            or hasBareSecretPathForMode(pendingRemote, CT_MD)
        all {
          localBody' = rev,
          localFormat' = localFormat,
          hasId' = hasId,
          listedSecret' = listedSecret,
          currentUser' = currentUser,
          localMetaRev' = remoteMetaRev,
          unchangedSidecars,
          hasBase' = true,
          localBase' = rev,
          remoteBase' = remoteBody,
          savedContentType' = CT_MD,
          metaBaseRev' = remoteMetaRev,
          pendingCreate' = false,
          bodySent' = false,
          hasPendingRemote' = false,
          pendingRemote' = "",
          rememberedSecret' = nextRemembered,
          clearPendingPublish,
          unchangedRemoteState,
          recordGhost("pull", "", "pulled"),
        }
      }
    }
  }

  action mergeResolved: bool = {
    val promotedLocal = serverReverse(pendingRemote)
    val nextRemembered =
      rememberedSecret or hasBareSecretPathForMode(remoteBase, savedBaseCT(savedContentType))
    all {
      sidecarsExist,
      hasId,
      hasBase,
      hasPendingRemote,
      savedContentType != CT_HTML,
      not(hasPendingPublish),
      localBody' = localBody,
      hasId' = hasId,
      listedSecret' = listedSecret,
      unchangedLocalMeta,
      sidecarsExist' = false,
      sidecarRemote' = "",
      sidecarBase' = "",
      sidecarListedSecret' = false,
      hasBase' = true,
      localBase' = promotedLocal,
      remoteBase' = pendingRemote,
      savedContentType' = CT_MD,
      metaBaseRev' = metaBaseRev,
      pendingCreate' = false,
      bodySent' = false,
      hasPendingRemote' = false,
      pendingRemote' = "",
      rememberedSecret' = nextRemembered,
      unchangedPendingPublish,
      unchangedRemoteState,
      recordGhost("merge_resolved", "", "resolved"),
    }
  }

  pure val SAMPLE_REMOTE_BODIES =
    Set("", "v1|norm", "v2|norm", "v3|norm", "link_secret|norm", "v1|html", "v2|html", "web_html_1", "link_secret|html", BACKTICK_HTML_SECRET)

  action step = any {
    nondet t = ALL_TEXTS.oneOf()
    localEdit(t),
    nondet fm = ALL_FORMATS.oneOf()
    setLocalFormat(fm),
    nondet l = Set(true, false).oneOf()
    setListedSecret(l),
    stripLegacyContentType,
    nondet u = ALL_USERS.oneOf()
    switchToSecondMachine(u),
    nondet wt = SAMPLE_REMOTE_BODIES.oneOf()
    webEdit(wt),
    webEditMeta,
    nondet lk = Set(true, false).oneOf()
    webSetLock(lk),
    nondet hm = Set(true, false).oneOf()
    webSetHtml(hm),
    nondet ow = ALL_USERS.oneOf()
    webSetOwner(ow),
    nondet pr = Set(true, false).oneOf()
    pushCreateInterrupted(pr),
    nondet hc = Set(true, false).oneOf()
    nondet cf = SAMPLE_REMOTE_BODIES.oneOf()
    nondet pr2 = Set(true, false).oneOf()
    pushPublishInterrupted(hc, cf, pr2),
    nondet f = Set(true, false).oneOf()
    push(f),
    nondet cf2 = SAMPLE_REMOTE_BODIES.oneOf()
    pushWith(false, true, cf2),
    nondet cw = Set("v2|norm", "v3|norm").oneOf()
    pushMidRetryConflict(cw),
    nondet hc3 = Set(true, false).oneOf()
    nondet cf3 = SAMPLE_REMOTE_BODIES.oneOf()
    nondet rw = Set("web_html_1", "v1|html", "v2|html").oneOf()
    nondet rhm = Set(true, false).oneOf()
    nondet rlk = Set(true, false).oneOf()
    nondet row = ALL_USERS.oneOf()
    pushPrePatchReGetBlocked(hc3, cf3, rw, rhm, rlk, row),
    nondet hc4 = Set(true, false).oneOf()
    nondet cf4 = SAMPLE_REMOTE_BODIES.oneOf()
    nondet rw2 = Set("web_html_1", "web_html_2").oneOf()
    nondet rlk2 = Set(true, false).oneOf()
    pushPostReGetRace(hc4, cf4, rw2, rlk2),
    pull,
    mergeResolved,
  }

  pure def isCliPushAction(act: str): bool =
    act == "push"
      or act == "push_force"
      or act == "push_confirm"
      or act == "push_publish_lost_response"
      or act == "push_publish_fail_before_patch"
      or act == "push_pre_patch_reget_blocked"

  val invNoPushToLocked =
    (prevRemoteLocked and isCliPushAction(lastAction) and not(postReGetRaceOccurred))
      implies (remoteBody == prevRemoteBody and remoteIsHtml == prevRemoteIsHtml and lastOutcome != "pushed")

  val invNoUnauthorizedHtmlWrite =
    (prevRemoteIsHtml
      and prevRemoteBody != ""
      and isCliPushAction(lastAction)
      and not(postReGetRaceOccurred)
      and (remoteBody != prevRemoteBody or remoteIsHtml != prevRemoteIsHtml or lastOutcome == "pushed"))
      implies (
        not(prevRemoteLocked)
          and prevLocalFormat != FORMAT_OMITTED
          and (
            (prevHasBase and prevSavedContentType == CT_HTML and prevRemoteBody == prevRemoteBase and (not(prevHasPendingPublish) or prevPendingUnchanged))
              or (prevHasBase and prevSavedContentType == CT_HTML and (not(prevHasPendingPublish) or prevPendingUnchanged) and prevHasConfirm and prevConfirmBody == prevRemoteBody)
              or (prevLocalFormat == FORMAT_HTML and prevRemoteOwner == prevCurrentUser and (not(prevHasPendingPublish) or prevPendingUnchanged) and prevHasConfirm and prevConfirmBody == prevRemoteBody)
              or (
                prevHasPendingPublish
                  and prevPendingPublishUser == prevCurrentUser
                  and prevHasConfirm
                  and prevConfirmBody == prevRemoteBody
                  and (
                    (prevLocalFormat == FORMAT_HTML and prevPendingPublishTargetCT == CT_HTML)
                      or (prevLocalFormat == FORMAT_MARKDOWN and prevPendingPublishTargetCT == CT_MD)
                  )
              )
          )
      )

  val invUnregisteredHtmlRefusesForce =
    (prevRemoteIsHtml
      and prevRemoteBody != ""
      and not(prevRemoteLocked)
      and not(prevHasBase and prevSavedContentType == CT_HTML)
      and not(prevHasPendingPublish)
      and (lastAction == "push" or lastAction == "push_force"))
      implies (remoteBody == prevRemoteBody and remoteIsHtml == prevRemoteIsHtml and lastOutcome == "refused_html")

  val invCollabNeverAdoptsUnregisteredHtml =
    (prevRemoteIsHtml
      and prevRemoteBody != ""
      and not(prevRemoteLocked)
      and not(prevHasBase and prevSavedContentType == CT_HTML)
      and not(prevHasPendingPublish)
      and prevRemoteOwner != prevCurrentUser
      and isCliPushAction(lastAction))
      implies (remoteBody == prevRemoteBody and remoteIsHtml == prevRemoteIsHtml and lastOutcome == "refused_html")

  val invOmittedFormatNeverOverwritesHtml =
    (((prevRemoteIsHtml and prevRemoteBody != "") or (prevHasBase and prevSavedContentType == CT_HTML))
      and prevLocalFormat == FORMAT_OMITTED
      and isCliPushAction(lastAction)
      and not(postReGetRaceOccurred))
      implies (remoteBody == prevRemoteBody and remoteIsHtml == prevRemoteIsHtml and lastOutcome != "pushed")

  val invDeletedRemoteHtmlNotTreatedAsNew =
    (prevRemoteIsHtml
      and prevRemoteBody == ""
      and prevHasBase
      and prevRemoteBase != ""
      and (
        lastAction == "push"
          or (lastAction == "push_force" and (prevSavedContentType == CT_HTML or prevLocalFormat == FORMAT_HTML))
      ))
      implies (remoteBody == prevRemoteBody and remoteIsHtml == prevRemoteIsHtml and lastOutcome != "pushed")

  val invHtmlPullNeverMutatesLocalBody =
    (lastAction == "pull" and ((prevRemoteIsHtml and prevRemoteBody != "") or prevSavedContentType == CT_HTML))
      implies (localBody == prevLocalBody)

  val invHtmlPullPreservesBodyBase =
    (lastAction == "pull" and prevSavedContentType == CT_HTML)
      implies (remoteBase == prevRemoteBase and savedContentType == CT_HTML)

  val invAttachmentsNeverDeleted =
    prevSecretUploaded implies secretUploaded

  val invNoUnauthorizedRemoteLinkedUpload =
    (not(prevSecretUploaded) and secretUploaded and prevSecretRemoteLinked)
      implies prevListedSecret

  val invConflictEndsResume =
    hasPendingRemote implies not(isResuming(hasBase, pendingCreate, bodySent, hasPendingRemote))

  val invNoSidecarClobber =
    (prevSidecarsExist and lastAction != "merge_resolved" and lastAction != "switch_machine")
      implies (sidecarsExist and sidecarRemote == prevSidecarRemote and sidecarBase == prevSidecarBase)

  val invHtmlNeverCreatesMarkdownSidecars =
    (prevSavedContentType == CT_HTML
      or (prevRemoteIsHtml and prevRemoteBody != "")
      or (prevLocalFormat == FORMAT_HTML and isCliPushAction(lastAction)))
      implies not(sidecarsExist and not(prevSidecarsExist))

  val allSafetyInvariants = and {
    invNoPushToLocked,
    invNoUnauthorizedHtmlWrite,
    invUnregisteredHtmlRefusesForce,
    invCollabNeverAdoptsUnregisteredHtml,
    invOmittedFormatNeverOverwritesHtml,
    invDeletedRemoteHtmlNotTreatedAsNew,
    invHtmlPullNeverMutatesLocalBody,
    invHtmlPullPreservesBodyBase,
    invAttachmentsNeverDeleted,
    invNoUnauthorizedRemoteLinkedUpload,
    invConflictEndsResume,
    invNoSidecarClobber,
    invHtmlNeverCreatesMarkdownSidecars,
  }

  run traceCreateResumeConflictMergeTest =
    init
      .then(pushCreateInterrupted(true))
      .then(pushMidRetryConflict("v2|norm"))
      .then(push(false))
      .then(mergeResolved)
      .then(localEdit("v3"))
      .then(push(false))

  run traceLockAndHtmlRefusalTest =
    init
      .then(push(false))
      .then(webSetLock(true))
      .then(localEdit("v2"))
      .then(push(true))
      .then(webSetLock(false))
      .then(webSetHtml(true))
      .then(push(true))
      .then(pull)
      .then(webSetHtml(false))
      .then(push(false))

  run traceRemoteLinkedPathProtectionTest =
    init
      .then(push(false))
      .then(webEdit("link_secret|norm"))
      .then(pull)
      .then(webEdit("v2|norm"))
      .then(push(true))
      .then(setListedSecret(true))
      .then(push(false))

  run traceInterruptedBeforePatchDetectsWebEditTest =
    init
      .then(pushCreateInterrupted(false))
      .then(webEdit("v2|norm"))
      .then(push(false))
      .then(mergeResolved)
      .then(push(false))

  run traceSidecarCollisionRefusedTest =
    init
      .then(push(false))
      .then(webEdit("v2|norm"))
      .then(localEdit("v3"))
      .then(push(false))
      .then(webEdit("v3|norm"))

      .then(push(false))
      .then(mergeResolved)
      .then(push(false))

  run traceHtmlNewRepushAndWebConflictTest =
    init
      .then(setLocalFormat(FORMAT_HTML))
      .then(push(false))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and remoteIsHtml and savedContentType == CT_HTML and remoteBody == "v1|html")
      .then(localEdit("v2"))
      .then(push(false))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and remoteBody == "v2|html")
      .then(webEdit("web_html_1"))
      .then(push(true))
      .expect(allSafetyInvariants and lastOutcome == "conflict_html" and remoteBody == "web_html_1" and not(sidecarsExist))
      .then(pushWith(false, true, "v2|html"))
      .expect(allSafetyInvariants and lastOutcome == "conflict_html" and remoteBody == "web_html_1")
      .then(pushWith(false, true, "web_html_1"))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and remoteBody == "v2|html")

      .then(localEdit("v3"))
      .then(pushPublishInterrupted(false, "", true))
      .expect(allSafetyInvariants and not(hasPendingPublish) and remoteBody == "v3|html" and remoteBase == "v2|html")
      .then(push(true))
      .expect(allSafetyInvariants and lastOutcome == "conflict_html")
      .then(pushWith(false, true, "v3|html"))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and remoteBase == "v3|html")

  run traceHtmlLegacyMigrateEmptyOmitAndRevertTest =
    init
      .then(push(false))
      .then(stripLegacyContentType)
      .expect(allSafetyInvariants and hasBase and savedContentType == CT_NONE and remoteBase == "v1|norm")
      .then(setLocalFormat(FORMAT_HTML))
      .then(push(false))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and remoteIsHtml and savedContentType == CT_HTML and remoteBody == "v1|html")

      .then(localEdit(""))
      .then(push(false))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and remoteIsHtml and remoteBody == "" and savedContentType == CT_HTML)
      .then(setLocalFormat(FORMAT_OMITTED))
      .then(push(true))
      .expect(allSafetyInvariants and lastOutcome == "refused_html" and remoteIsHtml)
      .then(localEdit("v1"))
      .then(setLocalFormat(FORMAT_MARKDOWN))
      .then(push(false))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and not(remoteIsHtml) and savedContentType == CT_MD and remoteBody == "v1|norm")
      .then(webEdit("v2|norm"))
      .then(pull)
      .expect(allSafetyInvariants and lastOutcome == "pulled" and localBody == "v2")

  run traceHtmlPendingPublishRecoveryTest =
    init

      .then(setLocalFormat(FORMAT_HTML))
      .then(pushPublishInterrupted(false, "", true))
      .expect(allSafetyInvariants and hasPendingPublish and pendingPublishTargetCT == CT_HTML and remoteIsHtml and remoteBody == "v1|html")
      .then(setLocalFormat(FORMAT_MARKDOWN))
      .then(pushWith(false, true, "v1|html"))
      .expect(allSafetyInvariants and lastOutcome == "refused_pending_publish" and remoteIsHtml)
      .then(setLocalFormat(FORMAT_HTML))
      .then(pushWith(false, true, "v1|html"))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and not(hasPendingPublish) and savedContentType == CT_HTML)

      .then(webEdit(BACKTICK_HTML_SECRET))
      .then(setLocalFormat(FORMAT_MARKDOWN))
      .then(pushPublishInterrupted(true, BACKTICK_HTML_SECRET, true))
      .expect(allSafetyInvariants and hasPendingPublish and pendingPublishTargetCT == CT_MD and savedContentType == CT_HTML and not(remoteIsHtml) and remoteBody == "v1|norm" and rememberedSecret)
      .then(localEdit(LINK_SECRET))
      .then(push(true))
      .expect(allSafetyInvariants and lastOutcome == "refused_pending_publish" and hasPendingPublish and not(secretUploaded))
      .then(pushWith(false, true, "v1|norm"))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and not(hasPendingPublish) and savedContentType == CT_MD and not(secretUploaded) and rememberedSecret)

  run traceHtmlSecondMachineAdoptionAndPrePatchReGetTest =
    init
      .then(setLocalFormat(FORMAT_HTML))
      .then(push(false))
      .then(switchToSecondMachine(USER_COLLAB))
      .expect(allSafetyInvariants and not(hasBase) and currentUser == USER_COLLAB)
      .then(push(true))
      .expect(allSafetyInvariants and lastOutcome == "refused_html")
      .then(pushWith(false, true, "v1|html"))
      .expect(allSafetyInvariants and lastOutcome == "refused_html")
      .then(switchToSecondMachine(USER_OWNER))
      .then(push(true))
      .expect(allSafetyInvariants and lastOutcome == "refused_html")

      .then(pushPrePatchReGetBlocked(true, "v1|html", "v1|html", true, false, USER_COLLAB))
      .expect(allSafetyInvariants and lastOutcome == "refused_html" and remoteOwner == USER_COLLAB and not(hasBase) and not(hasPendingPublish))

      .then(webSetOwner(USER_OWNER))
      .then(webEdit(BACKTICK_HTML_SECRET))
      .then(localEdit("v2"))
      .then(pushPublishInterrupted(true, BACKTICK_HTML_SECRET, true))
      .expect(allSafetyInvariants and not(hasPendingPublish) and not(hasBase) and remoteBody == "v2|html" and rememberedSecret)
      .then(webSetOwner(USER_COLLAB))
      .then(pushWith(false, true, "v2|html"))
      .expect(allSafetyInvariants and lastOutcome == "refused_html" and not(hasBase))
      .then(webSetOwner(USER_OWNER))
      .then(localEdit(LINK_SECRET))
      .then(pushWith(false, true, "v2|html"))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and hasBase and savedContentType == CT_HTML and remoteBase == "link_secret|html" and not(secretUploaded) and rememberedSecret)

      .then(pushPrePatchReGetBlocked(false, "", "link_secret|html", false, false, USER_OWNER))
      .expect(allSafetyInvariants and lastOutcome == "refused_mode_mismatch" and not(remoteIsHtml))

  run traceHtmlExternalModeSwitchAndDeletionTest =
    init
      .then(setLocalFormat(FORMAT_HTML))
      .then(push(false))
      .then(webSetHtml(false))
      .then(pull)
      .expect(allSafetyInvariants and lastOutcome == "refused_mode_mismatch" and localBody == "v1" and savedContentType == CT_HTML)
      .then(push(false))
      .expect(allSafetyInvariants and lastOutcome == "refused_mode_mismatch")

      .then(setLocalFormat(FORMAT_MARKDOWN))
      .then(pushPublishInterrupted(true, "v1|html", false))
      .expect(allSafetyInvariants and not(hasPendingPublish) and not(remoteIsHtml))
      .then(setLocalFormat(FORMAT_HTML))
      .then(webSetHtml(true))
      .then(webEdit(""))
      .then(push(false))
      .expect(allSafetyInvariants and lastOutcome == "conflict_html" and remoteBody == "")
      .then(pushWith(false, true, ""))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and remoteBody == "v1|html")

  run traceHtmlMetadataPullAndFormatAwareRemotePathTest =
    init
      .then(setLocalFormat(FORMAT_HTML))
      .then(push(false))
      .then(webEditMeta)
      .then(pull)
      .expect(allSafetyInvariants and lastOutcome == "pulled_meta" and localMetaRev == 1 and metaBaseRev == 1 and localBody == "v1" and remoteBase == "v1|html")

      .then(webEdit(BACKTICK_HTML_SECRET))
      .then(localEdit("link_secret"))
      .then(pushWith(false, true, BACKTICK_HTML_SECRET))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and not(secretUploaded) and rememberedSecret)
      .then(setListedSecret(true))
      .then(push(false))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and secretUploaded)

  run traceHtmlPrePatchReGetAndPostReGetRaceTest =
    init
      .then(setLocalFormat(FORMAT_HTML))
      .then(push(false))
      .then(localEdit("v2"))
      .then(pushPrePatchReGetBlocked(false, "", "web_html_1", true, false, USER_OWNER))
      .expect(allSafetyInvariants and lastOutcome == "conflict_html" and remoteBody == "web_html_1")
      .then(webSetLock(true))
      .then(pushWith(false, true, "web_html_1"))
      .expect(allSafetyInvariants and lastOutcome == "refused_locked" and remoteBody == "web_html_1")
      .then(webSetLock(false))
      .then(pushWith(false, true, "web_html_1"))
      .expect(allSafetyInvariants and lastOutcome == "pushed" and remoteBody == "v2|html")
      .then(localEdit("v3"))
      .then(pushPostReGetRace(false, "", "web_html_2", true))
      .expect(allSafetyInvariants and lastOutcome == "pushed_raced" and postReGetRaceOccurred and remoteLocked)
}
