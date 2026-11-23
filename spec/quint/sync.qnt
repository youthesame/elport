// -*- mode: Bluespec; -*-
// Formal protocol model for elport's Markdown synchronization state machine.
// Models two-form bases (local_base vs server-normalized remote_base),
// interrupted creation & lost PATCH responses, Web UI edits, locks, HTML mode,
// conflict sidecars & merge promotion, remote-linked path protection, and
// monotonic attachment retention (never deleting uploads).
module elport_sync {
  pure val NORMAL_TEXTS = Set("v1", "v2", "v3")
  pure val LINK_SECRET = "link_secret"
  pure val ALL_TEXTS = NORMAL_TEXTS.union(Set(LINK_SECRET))

  // Server normalization is non-identity so same-form comparison is essential.
  pure def serverNorm(localText: str): str =
    if (localText == "v1") "v1|norm"
    else if (localText == "v2") "v2|norm"
    else if (localText == "v3") "v3|norm"
    else if (localText == "link_secret") "link_secret|norm"
    else ""

  pure def serverReverse(remoteText: str): str =
    if (remoteText == "v1|norm") "v1"
    else if (remoteText == "v2|norm") "v2"
    else if (remoteText == "v3|norm") "v3"
    else if (remoteText == "link_secret|norm") "link_secret"
    else ""

  pure def hasBareSecretPath(body: str): bool =
    body == LINK_SECRET or body == "link_secret|norm"

  // Local state
  var localBody: str
  var hasId: bool
  var listedSecret: bool
  var sidecarsExist: bool

  // Persisted sync base state (~/.config/elport/state/<hash>)
  var hasBase: bool
  var localBase: str
  var remoteBase: str
  var pendingCreate: bool
  var bodySent: bool
  var hasPendingRemote: bool
  var pendingRemote: str
  var rememberedSecret: bool

  // Remote eLabFTW server state
  var remoteExists: bool
  var remoteBody: str
  var remoteLocked: bool
  var remoteIsHtml: bool
  var secretUploaded: bool

  // Ghost / trace observation variables for invariants and Go replay
  var lastAction: str
  var lastArg: str
  var lastOutcome: str
  var prevRemoteBody: str
  var prevRemoteLocked: bool
  var prevRemoteIsHtml: bool
  var prevSecretUploaded: bool
  var prevListedSecret: bool
  var prevSecretRemoteLinked: bool

  pure def isResuming(
    pHasBase: bool,
    pPendingCreate: bool,
    pBodySent: bool,
    pHasPendingRemote: bool
  ): bool =
    pHasBase and pPendingCreate and pBodySent and not(pHasPendingRemote)

  pure def isSecretRemoteLinked(
    pRemoteBody: str,
    pRemoteBase: str,
    pPendingRemote: str,
    pRememberedSecret: bool
  ): bool =
    pRememberedSecret
      or hasBareSecretPath(pRemoteBody)
      or hasBareSecretPath(pRemoteBase)
      or hasBareSecretPath(pPendingRemote)

  action init = all {
    localBody' = "v1",
    hasId' = false,
    listedSecret' = false,
    sidecarsExist' = false,

    hasBase' = false,
    localBase' = "",
    remoteBase' = "",
    pendingCreate' = false,
    bodySent' = false,
    hasPendingRemote' = false,
    pendingRemote' = "",
    rememberedSecret' = false,

    remoteExists' = false,
    remoteBody' = "",
    remoteLocked' = false,
    remoteIsHtml' = false,
    secretUploaded' = false,

    lastAction' = "init",
    lastArg' = "",
    lastOutcome' = "ok",
    prevRemoteBody' = "",
    prevRemoteLocked' = false,
    prevRemoteIsHtml' = false,
    prevSecretUploaded' = false,
    prevListedSecret' = false,
    prevSecretRemoteLinked' = false,
  }

  action recordGhost(act: str, arg: str, outcome: str): bool = all {
    lastAction' = act,
    lastArg' = arg,
    lastOutcome' = outcome,
    prevRemoteBody' = remoteBody,
    prevRemoteLocked' = remoteLocked,
    prevRemoteIsHtml' = remoteIsHtml,
    prevSecretUploaded' = secretUploaded,
    prevListedSecret' = listedSecret,
    prevSecretRemoteLinked' = isSecretRemoteLinked(remoteBody, remoteBase, pendingRemote, rememberedSecret),
  }

  action localEdit(newBody: str): bool = all {
    newBody != localBody,
    localBody' = newBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    sidecarsExist' = sidecarsExist,
    hasBase' = hasBase,
    localBase' = localBase,
    remoteBase' = remoteBase,
    pendingCreate' = pendingCreate,
    bodySent' = bodySent,
    hasPendingRemote' = hasPendingRemote,
    pendingRemote' = pendingRemote,
    rememberedSecret' = rememberedSecret,
    remoteExists' = remoteExists,
    remoteBody' = remoteBody,
    remoteLocked' = remoteLocked,
    remoteIsHtml' = remoteIsHtml,
    secretUploaded' = secretUploaded,
    recordGhost("local_edit", newBody, "ok"),
  }

  action setListedSecret(listed: bool): bool = all {
    listed != listedSecret,
    listedSecret' = listed,
    localBody' = localBody,
    hasId' = hasId,
    sidecarsExist' = sidecarsExist,
    hasBase' = hasBase,
    localBase' = localBase,
    remoteBase' = remoteBase,
    pendingCreate' = pendingCreate,
    bodySent' = bodySent,
    hasPendingRemote' = hasPendingRemote,
    pendingRemote' = pendingRemote,
    rememberedSecret' = rememberedSecret,
    remoteExists' = remoteExists,
    remoteBody' = remoteBody,
    remoteLocked' = remoteLocked,
    remoteIsHtml' = remoteIsHtml,
    secretUploaded' = secretUploaded,
    recordGhost(if (listed) "list_secret" else "unlist_secret", "", "ok"),
  }

  action webEdit(newBody: str): bool = all {
    remoteExists,
    not(remoteLocked),
    newBody != remoteBody,
    remoteBody' = newBody,
    localBody' = localBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    sidecarsExist' = sidecarsExist,
    hasBase' = hasBase,
    localBase' = localBase,
    remoteBase' = remoteBase,
    pendingCreate' = pendingCreate,
    bodySent' = bodySent,
    hasPendingRemote' = hasPendingRemote,
    pendingRemote' = pendingRemote,
    rememberedSecret' = rememberedSecret,
    remoteExists' = remoteExists,
    remoteLocked' = remoteLocked,
    remoteIsHtml' = remoteIsHtml,
    secretUploaded' = secretUploaded,
    recordGhost("web_edit", newBody, "ok"),
  }

  action webSetLock(locked: bool): bool = all {
    remoteExists,
    locked != remoteLocked,
    remoteLocked' = locked,
    localBody' = localBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    sidecarsExist' = sidecarsExist,
    hasBase' = hasBase,
    localBase' = localBase,
    remoteBase' = remoteBase,
    pendingCreate' = pendingCreate,
    bodySent' = bodySent,
    hasPendingRemote' = hasPendingRemote,
    pendingRemote' = pendingRemote,
    rememberedSecret' = rememberedSecret,
    remoteExists' = remoteExists,
    remoteBody' = remoteBody,
    remoteIsHtml' = remoteIsHtml,
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
    sidecarsExist' = sidecarsExist,
    hasBase' = hasBase,
    localBase' = localBase,
    remoteBase' = remoteBase,
    pendingCreate' = pendingCreate,
    bodySent' = bodySent,
    hasPendingRemote' = hasPendingRemote,
    pendingRemote' = pendingRemote,
    rememberedSecret' = rememberedSecret,
    remoteExists' = remoteExists,
    remoteBody' = remoteBody,
    remoteLocked' = remoteLocked,
    secretUploaded' = secretUploaded,
    recordGhost(if (htmlMode) "web_html_on" else "web_html_off", "", "ok"),
  }

  action pushCreateInterrupted(patchReachedServer: bool): bool = {
    val stored = if (patchReachedServer) serverNorm(localBody) else ""
    val uploadedNow =
      patchReachedServer and (listedSecret or hasBareSecretPath(localBody))
    all {
      not(hasId),
      not(remoteExists),
      localBody != "",
      hasId' = true,
      remoteExists' = true,
      remoteBody' = stored,
      hasBase' = true,
      localBase' = "",
      remoteBase' = "",
      pendingCreate' = true,
      bodySent' = patchReachedServer,
      hasPendingRemote' = false,
      pendingRemote' = "",
      rememberedSecret' = rememberedSecret,
      localBody' = localBody,
      listedSecret' = listedSecret,
      sidecarsExist' = sidecarsExist,
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

  action push(force: bool): bool = {
    val actName = if (force) "push_force" else "push"
    val resuming = isResuming(hasBase, pendingCreate, bodySent, hasPendingRemote)
    val secretLinked = isSecretRemoteLinked(remoteBody, remoteBase, pendingRemote, rememberedSecret)
    val shouldUploadSecret =
      listedSecret or (hasBareSecretPath(localBody) and not(secretLinked))
    if (remoteExists and remoteIsHtml and remoteBody != "") all {
      localBody' = localBody,
      hasId' = hasId,
      listedSecret' = listedSecret,
      sidecarsExist' = sidecarsExist,
      hasBase' = hasBase,
      localBase' = localBase,
      remoteBase' = remoteBase,
      pendingCreate' = pendingCreate,
      bodySent' = bodySent,
      hasPendingRemote' = hasPendingRemote,
      pendingRemote' = pendingRemote,
      rememberedSecret' = rememberedSecret,
      remoteExists' = remoteExists,
      remoteBody' = remoteBody,
      remoteLocked' = remoteLocked,
      remoteIsHtml' = remoteIsHtml,
      secretUploaded' = secretUploaded,
      recordGhost(actName, "", "refused_html"),
    } else if (remoteExists and remoteLocked) all {
      localBody' = localBody,
      hasId' = hasId,
      listedSecret' = listedSecret,
      sidecarsExist' = sidecarsExist,
      hasBase' = hasBase,
      localBase' = localBase,
      remoteBase' = remoteBase,
      pendingCreate' = pendingCreate,
      bodySent' = bodySent,
      hasPendingRemote' = hasPendingRemote,
      pendingRemote' = pendingRemote,
      rememberedSecret' = rememberedSecret,
      remoteExists' = remoteExists,
      remoteBody' = remoteBody,
      remoteLocked' = remoteLocked,
      remoteIsHtml' = remoteIsHtml,
      secretUploaded' = secretUploaded,
      recordGhost(actName, "", "refused_locked"),
    } else if (hasId and not(hasBase) and not(force)) all {
      localBody' = localBody,
      hasId' = hasId,
      listedSecret' = listedSecret,
      sidecarsExist' = sidecarsExist,
      hasBase' = hasBase,
      localBase' = localBase,
      remoteBase' = remoteBase,
      pendingCreate' = pendingCreate,
      bodySent' = bodySent,
      hasPendingRemote' = hasPendingRemote,
      pendingRemote' = pendingRemote,
      rememberedSecret' = rememberedSecret,
      remoteExists' = remoteExists,
      remoteBody' = remoteBody,
      remoteLocked' = remoteLocked,
      remoteIsHtml' = remoteIsHtml,
      secretUploaded' = secretUploaded,
      recordGhost(actName, "", "refused_no_base"),
    } else if (hasId and hasBase and not(resuming) and remoteBody != remoteBase and not(force)) all {
      localBody' = localBody,
      hasId' = hasId,
      listedSecret' = listedSecret,
      sidecarsExist' = true,
      hasBase' = hasBase,
      localBase' = localBase,
      remoteBase' = remoteBase,
      pendingCreate' = pendingCreate,
      bodySent' = bodySent,
      hasPendingRemote' = true,
      pendingRemote' = remoteBody,
      rememberedSecret' = (rememberedSecret or hasBareSecretPath(pendingRemote)),
      remoteExists' = remoteExists,
      remoteBody' = remoteBody,
      remoteLocked' = remoteLocked,
      remoteIsHtml' = remoteIsHtml,
      secretUploaded' = secretUploaded,
      recordGhost(actName, "", "conflict"),
    } else {
      val nextRemoteBody = serverNorm(localBody)
      val nextRemembered =
        rememberedSecret
          or hasBareSecretPath(remoteBase)
          or hasBareSecretPath(pendingRemote)
          or hasBareSecretPath(remoteBody)
      val nextUploaded = secretUploaded or shouldUploadSecret
      all {
        localBody' = localBody,
        hasId' = true,
        listedSecret' = listedSecret,
        sidecarsExist' = sidecarsExist,
        hasBase' = true,
        localBase' = localBody,
        remoteBase' = nextRemoteBody,
        pendingCreate' = false,
        bodySent' = false,
        hasPendingRemote' = false,
        pendingRemote' = "",
        rememberedSecret' = nextRemembered,
        remoteExists' = true,
        remoteBody' = nextRemoteBody,
        remoteLocked' = remoteLocked,
        remoteIsHtml' = false,
        secretUploaded' = nextUploaded,
        recordGhost(actName, "", "pushed"),
      }
    }
  }

  action pushMidRetryConflict(concurrentWebBody: str): bool = all {
    hasId,
    remoteExists,
    not(remoteLocked),
    not(remoteIsHtml and remoteBody != ""),
    isResuming(hasBase, pendingCreate, bodySent, hasPendingRemote),
    concurrentWebBody != remoteBody,
    localBody' = localBody,
    hasId' = hasId,
    listedSecret' = listedSecret,
    sidecarsExist' = true,
    hasBase' = hasBase,
    localBase' = localBase,
    remoteBase' = remoteBase,
    pendingCreate' = pendingCreate,
    bodySent' = bodySent,
    hasPendingRemote' = true,
    pendingRemote' = concurrentWebBody,
    rememberedSecret' = (rememberedSecret or hasBareSecretPath(pendingRemote)),
    remoteExists' = remoteExists,
    remoteBody' = concurrentWebBody,
    remoteLocked' = remoteLocked,
    remoteIsHtml' = remoteIsHtml,
    secretUploaded' = secretUploaded,
    recordGhost("push_mid_retry_conflict", concurrentWebBody, "conflict"),
  }

  action pull: bool = all {
    hasId,
    remoteExists,
    if (remoteIsHtml and remoteBody != "") all {
      localBody' = localBody,
      hasId' = hasId,
      listedSecret' = listedSecret,
      sidecarsExist' = sidecarsExist,
      hasBase' = hasBase,
      localBase' = localBase,
      remoteBase' = remoteBase,
      pendingCreate' = pendingCreate,
      bodySent' = bodySent,
      hasPendingRemote' = hasPendingRemote,
      pendingRemote' = pendingRemote,
      rememberedSecret' = rememberedSecret,
      remoteExists' = remoteExists,
      remoteBody' = remoteBody,
      remoteLocked' = remoteLocked,
      remoteIsHtml' = remoteIsHtml,
      secretUploaded' = secretUploaded,
      recordGhost("pull", "", "refused_html"),
    } else if (hasBase and localBody != localBase and remoteBody == remoteBase) all {
      localBody' = localBody,
      hasId' = hasId,
      listedSecret' = listedSecret,
      sidecarsExist' = sidecarsExist,
      hasBase' = hasBase,
      localBase' = localBase,
      remoteBase' = remoteBase,
      pendingCreate' = pendingCreate,
      bodySent' = bodySent,
      hasPendingRemote' = hasPendingRemote,
      pendingRemote' = pendingRemote,
      rememberedSecret' = rememberedSecret,
      remoteExists' = remoteExists,
      remoteBody' = remoteBody,
      remoteLocked' = remoteLocked,
      remoteIsHtml' = remoteIsHtml,
      secretUploaded' = secretUploaded,
      recordGhost("pull", "", "up_to_date"),
    } else if (hasBase and localBody != localBase and remoteBody != remoteBase) all {
      localBody' = localBody,
      hasId' = hasId,
      listedSecret' = listedSecret,
      sidecarsExist' = true,
      hasBase' = hasBase,
      localBase' = localBase,
      remoteBase' = remoteBase,
      pendingCreate' = pendingCreate,
      bodySent' = bodySent,
      hasPendingRemote' = true,
      pendingRemote' = remoteBody,
      rememberedSecret' = (rememberedSecret or hasBareSecretPath(pendingRemote)),
      remoteExists' = remoteExists,
      remoteBody' = remoteBody,
      remoteLocked' = remoteLocked,
      remoteIsHtml' = remoteIsHtml,
      secretUploaded' = secretUploaded,
      recordGhost("pull", "", "conflict"),
    } else {
      val rev = serverReverse(remoteBody)
      val nextRemembered =
        rememberedSecret
          or hasBareSecretPath(remoteBase)
          or hasBareSecretPath(pendingRemote)
      all {
        localBody' = rev,
        hasId' = hasId,
        listedSecret' = listedSecret,
        sidecarsExist' = sidecarsExist,
        hasBase' = true,
        localBase' = rev,
        remoteBase' = remoteBody,
        pendingCreate' = false,
        bodySent' = false,
        hasPendingRemote' = false,
        pendingRemote' = "",
        rememberedSecret' = nextRemembered,
        remoteExists' = remoteExists,
        remoteBody' = remoteBody,
        remoteLocked' = remoteLocked,
        remoteIsHtml' = remoteIsHtml,
        secretUploaded' = secretUploaded,
        recordGhost("pull", "", "pulled"),
      }
    }
  }

  action mergeResolved: bool = {
    val promotedLocal = serverReverse(pendingRemote)
    val nextRemembered = rememberedSecret or hasBareSecretPath(remoteBase)
    all {
      sidecarsExist,
      hasId,
      hasBase,
      hasPendingRemote,
      localBody' = localBody,
      hasId' = hasId,
      listedSecret' = listedSecret,
      sidecarsExist' = false,
      hasBase' = true,
      localBase' = promotedLocal,
      remoteBase' = pendingRemote,
      pendingCreate' = false,
      bodySent' = false,
      hasPendingRemote' = false,
      pendingRemote' = "",
      rememberedSecret' = nextRemembered,
      remoteExists' = remoteExists,
      remoteBody' = remoteBody,
      remoteLocked' = remoteLocked,
      remoteIsHtml' = remoteIsHtml,
      secretUploaded' = secretUploaded,
      recordGhost("merge_resolved", "", "resolved"),
    }
  }

  action step = any {
    nondet t = ALL_TEXTS.oneOf()
    localEdit(t),
    nondet l = Set(true, false).oneOf()
    setListedSecret(l),
    nondet wt = Set("v1|norm", "v2|norm", "v3|norm", "link_secret|norm").oneOf()
    webEdit(wt),
    nondet lk = Set(true, false).oneOf()
    webSetLock(lk),
    nondet hm = Set(true, false).oneOf()
    webSetHtml(hm),
    nondet pr = Set(true, false).oneOf()
    pushCreateInterrupted(pr),
    nondet f = Set(true, false).oneOf()
    push(f),
    nondet cw = Set("v2|norm", "v3|norm").oneOf()
    pushMidRetryConflict(cw),
    pull,
    mergeResolved,
  }

  // --- Safety Invariants ---

  val invNoPushToLocked =
    (prevRemoteLocked and (lastAction == "push" or lastAction == "push_force"))
      implies (remoteBody == prevRemoteBody and lastOutcome != "pushed")

  val invNoSyncOnNonEmptyHtml =
    (prevRemoteIsHtml and prevRemoteBody != "" and (lastAction == "push" or lastAction == "push_force" or lastAction == "pull"))
      implies (remoteBody == prevRemoteBody and lastOutcome == "refused_html")

  val invAttachmentsNeverDeleted =
    prevSecretUploaded implies secretUploaded

  val invNoUnauthorizedRemoteLinkedUpload =
    (not(prevSecretUploaded) and secretUploaded and prevSecretRemoteLinked)
      implies prevListedSecret

  val invConflictEndsResume =
    hasPendingRemote implies not(isResuming(hasBase, pendingCreate, bodySent, hasPendingRemote))

  val allSafetyInvariants = and {
    invNoPushToLocked,
    invNoSyncOnNonEmptyHtml,
    invAttachmentsNeverDeleted,
    invNoUnauthorizedRemoteLinkedUpload,
    invConflictEndsResume,
  }

  // --- Reproducible Trace Runs for ITF JSON Export ---

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
}
