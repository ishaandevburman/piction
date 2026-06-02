const statusEl = document.getElementById('status')
const roomLabelEl = document.getElementById('room-label')
const appEl = document.getElementById('app')
const joinModal = document.getElementById('join-modal')
const joinNameInput = document.getElementById('join-name-input')
const joinBtn = document.getElementById('join-btn')
const joinRoomLabel = document.getElementById('join-room-label')
const nameDisplay = document.getElementById('name-display')
const lobbyEl = document.getElementById('lobby')
const playerListEl = document.getElementById('player-list')
const startGameBtn = document.getElementById('start-game-btn')
const waitingMsg = document.getElementById('waiting-msg')
const gameArea = document.getElementById('game-area')
const gameStateLabel = document.getElementById('game-state-label')
const drawerLabel = document.getElementById('drawer-label')
const wordChoiceEl = document.getElementById('word-choice')
const pickingWaitEl = document.getElementById('picking-wait')
const wordButtonsEl = document.getElementById('word-buttons')
const wordInfoEl = document.getElementById('word-info')
const timerDisplay = document.getElementById('timer-display')
const drawToolbar = document.getElementById('draw-toolbar')
const canvas = document.getElementById('canvas')
const ctx = canvas.getContext('2d')
const chatMessages = document.getElementById('chat-messages')
const chatInput = document.getElementById('chat-input')
const clearCanvasBtn = document.getElementById('clear-canvas-btn')
const revealArea = document.getElementById('reveal-area')
const nextRoundBtn = document.getElementById('next-round-btn')
const waitingNextRound = document.getElementById('waiting-next-round')

const pathParts = window.location.pathname.split('/').filter(Boolean)
let roomId = (pathParts[0] === 'room' && pathParts[1]) ? pathParts[1] : 'default'
roomId = roomId.replace(/[^a-zA-Z0-9_-]/g, '_') || 'default'
roomLabelEl.textContent = roomId
joinRoomLabel.textContent = 'Room: ' + roomId

let userId = localStorage.getItem('piction_userId')
if (!userId) {
  userId = 'user_' + Math.random().toString(16).slice(2, 10)
  localStorage.setItem('piction_userId', userId)
}

let displayName = localStorage.getItem('piction_displayName') || ''
if (displayName) joinNameInput.value = displayName

let ws = null
let myUserId = ''
let players = []
let gameState = 'lobby'
let drawerId = ''
let myWord = ''
let wordOptions = []
let wordLen = 0
let difficulty = ''
let isDrawer = false
let isDrawing = false
let currentStroke = null
let strokeCount = 0
let remoteStrokes = {}
let currentTool = 'brush'
let currentColor = '#000000'
let currentBrushSize = 4
let timerInterval = null
let remainingSeconds = 0
let allStrokes = []

const PALETTE = ['#000000', '#ffffff', '#e74c3c', '#2ecc71', '#3498db', '#f1c40f', '#9b59b6', '#1abc9c', '#e67e22', '#8e44ad', '#95a5a6', '#d35400']

renderPalette()
joinModal.style.display = 'flex'
joinNameInput.focus()

function escapeHtml(s) {
  const d = document.createElement('div')
  d.textContent = s
  return d.innerHTML
}

function getCanvasPos(e) {
  const rect = canvas.getBoundingClientRect()
  return { x: e.clientX - rect.left, y: e.clientY - rect.top }
}

function resizeCanvas() {
  canvas.width = canvas.clientWidth
  canvas.height = canvas.clientHeight
  redrawCanvas()
}

function drawSegment(ctx, fromX, fromY, toX, toY, color, size) {
  ctx.lineWidth = size
  ctx.strokeStyle = color
  ctx.lineCap = 'round'
  ctx.lineJoin = 'round'
  ctx.beginPath()
  ctx.moveTo(fromX, fromY)
  ctx.lineTo(toX, toY)
  ctx.stroke()
}

function renderStrokes(strokes) {
  for (const s of strokes) {
    if (!s.points || s.points.length < 2) continue
    const color = s.tool === 'eraser' ? '#ffffff' : s.color
    ctx.lineWidth = s.brushSize
    ctx.strokeStyle = color
    ctx.lineCap = 'round'
    ctx.lineJoin = 'round'
    ctx.beginPath()
    ctx.moveTo(s.points[0].x, s.points[0].y)
    for (let i = 1; i < s.points.length; i++) {
      ctx.lineTo(s.points[i].x, s.points[i].y)
    }
    ctx.stroke()
  }
}

function clearCanvas() {
  ctx.clearRect(0, 0, canvas.width, canvas.height)
}

function redrawCanvas() {
  clearCanvas()
  renderStrokes(allStrokes)
  for (const id in remoteStrokes) {
    const rs = remoteStrokes[id]
    if (!rs.points || rs.points.length < 2) continue
    const color = rs.tool === 'eraser' ? '#ffffff' : rs.color
    ctx.lineWidth = rs.brushSize
    ctx.strokeStyle = color
    ctx.lineCap = 'round'
    ctx.lineJoin = 'round'
    ctx.beginPath()
    ctx.moveTo(rs.points[0].x, rs.points[0].y)
    for (let i = 1; i < rs.points.length; i++) {
      ctx.lineTo(rs.points[i].x, rs.points[i].y)
    }
    ctx.stroke()
  }
}

function renderPalette() {
  const container = document.getElementById('palette-swatches')
  container.innerHTML = PALETTE.map(c =>
    `<button class="color-swatch" style="background:${c}" data-color="${c}"></button>`
  ).join('')
  highlightSelectedColor()
}

function highlightSelectedColor() {
  document.querySelectorAll('.color-swatch').forEach(el => {
    el.classList.toggle('active', el.dataset.color === currentColor)
  })
}

function renderScoreboard() {
  const sorted = [...players].sort((a, b) => b.score - a.score)
  const html = sorted.map((p, i) => {
    const rank = ordinal(i + 1)
    const isYou = p.id === myUserId
    return `<div class="scoreboard-entry${isYou ? ' is-you' : ''}">
      <span class="rank">${rank}</span>
      <span class="name">${escapeHtml(p.displayName)}</span>
      <span class="score">${p.score}</span>
    </div>`
  }).join('')
  document.getElementById('scoreboard').innerHTML = '<h3>Scores</h3>' + html
}

function startTimer(duration) {
  stopTimer()
  remainingSeconds = duration
  updateTimerDisplay()
  timerInterval = setInterval(() => {
    remainingSeconds--
    updateTimerDisplay()
    if (remainingSeconds <= 0) {
      clearInterval(timerInterval)
      timerInterval = null
    }
  }, 1000)
}

function stopTimer() {
  if (timerInterval) {
    clearInterval(timerInterval)
    timerInterval = null
  }
  timerDisplay.textContent = ''
}

function updateTimerDisplay() {
  const m = Math.floor(remainingSeconds / 60)
  const s = remainingSeconds % 60
  timerDisplay.textContent = `${m}:${s.toString().padStart(2, '0')}`
}

function addChatMessage(user, message) {
  const el = document.createElement('div')
  el.className = 'chat-msg'
  el.innerHTML = `<strong>${escapeHtml(user)}:</strong> ${escapeHtml(message)}`
  chatMessages.appendChild(el)
  chatMessages.scrollTop = chatMessages.scrollHeight
}

function addCorrectGuessNotification(displayName, place) {
  const el = document.createElement('div')
  el.className = 'chat-msg correct-guess'
  el.textContent = `${displayName} guessed correctly! (${ordinal(place)})`
  chatMessages.appendChild(el)
  chatMessages.scrollTop = chatMessages.scrollHeight
}

function ordinal(n) {
  if (n === 1) return '1st'
  if (n === 2) return '2nd'
  if (n === 3) return '3rd'
  return n + 'th'
}

function connect(name) {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = location.host
  if (!host) {
    statusEl.textContent = 'Open via HTTP server (go run .)'
    statusEl.className = 'disconnected'
    return
  }
  statusEl.textContent = 'Connecting...'
  statusEl.className = 'disconnected'
  try {
    ws = new WebSocket(`${protocol}//${host}/ws?room=${roomId}`)
  } catch (e) {
    statusEl.textContent = 'Connection failed'
    statusEl.className = 'disconnected'
    setTimeout(() => connect(name), 2000)
    return
  }
  ws.onopen = () => {
    statusEl.textContent = 'Connected'
    statusEl.className = 'connected'
    ws.send(JSON.stringify({ type: 'join', userId, displayName: name }))
  }
  ws.onclose = () => {
    statusEl.textContent = 'Disconnected'
    statusEl.className = 'disconnected'
    setTimeout(() => connect(name), 2000)
  }
  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data)
    switch (msg.type) {
      case 'init':
        myUserId = msg.userId || ''
        players = msg.players || []
        gameState = msg.state || 'lobby'
        drawerId = msg.drawerId || ''
        wordLen = msg.wordLen || 0
        difficulty = msg.difficulty || ''
        myWord = msg.currentWord || ''
        wordOptions = msg.wordOptions || []
        if (msg.strokes) allStrokes = msg.strokes
        if (msg.duration && gameState === 'drawing') startTimer(msg.duration)
        if (gameState === 'reveal') {
          wordInfoEl.textContent = `The word was: ${escapeHtml(msg.word || '')}`
        }
        if (msg.correctGuessers) {
          msg.correctGuessers.forEach((uid, i) => {
            const p = players.find(pl => pl.id === uid)
            if (p) addCorrectGuessNotification(p.displayName, i + 1)
          })
        }
        renderState()
        break
      case 'players':
        players = msg.players || []
        renderState()
        break
      case 'game-state':
        gameState = msg.state
        drawerId = msg.drawerId || ''
        wordLen = msg.wordLen || 0
        difficulty = msg.difficulty || ''
        renderState()
        if (msg.state === 'drawing' && msg.duration) {
          startTimer(msg.duration)
        } else if (msg.state === 'reveal') {
          stopTimer()
          wordInfoEl.textContent = `The word was: ${escapeHtml(msg.word || '')}`
          if (msg.players) players = msg.players
          renderState()
        } else if (msg.state === 'picking') {
          stopTimer()
          resetCanvas()
          revealArea.style.display = 'none'
        } else if (msg.state === 'lobby') {
          stopTimer()
          resetCanvas()
          revealArea.style.display = 'none'
        }
        break
      case 'word-options':
        wordOptions = msg.words || []
        renderGameUI()
        break
      case 'your-word':
        myWord = msg.word || ''
        renderGameUI()
        break
      case 'draw':
        if (msg.action === 'clear') {
          allStrokes = []
          remoteStrokes = {}
          clearCanvas()
        } else if (msg.action === 'begin' && msg.stroke) {
          remoteStrokes[msg.stroke.id] = { ...msg.stroke, points: [], lastPoint: null }
        } else if (msg.action === 'point') {
          const rs = remoteStrokes[msg.strokeId]
          if (rs) {
            const pt = { x: msg.x, y: msg.y }
            if (rs.lastPoint) {
              const color = rs.tool === 'eraser' ? '#ffffff' : rs.color
              drawSegment(ctx, rs.lastPoint.x, rs.lastPoint.y, pt.x, pt.y, color, rs.brushSize)
            }
            rs.points.push(pt)
            rs.lastPoint = pt
          }
        } else if (msg.action === 'end') {
          const rs = remoteStrokes[msg.strokeId]
          if (rs) {
            allStrokes.push({ id: rs.id, color: rs.color, brushSize: rs.brushSize, tool: rs.tool, points: rs.points })
            delete remoteStrokes[msg.strokeId]
          }
        }
        break
      case 'chat':
        addChatMessage(msg.user || msg.userId, msg.message)
        break
      case 'correct-guess':
        addCorrectGuessNotification(msg.displayName, msg.place)
        break
    }
  }
}

function resetCanvas() {
  allStrokes = []
  remoteStrokes = {}
  clearCanvas()
  currentStroke = null
  isDrawing = false
  myWord = ''
  wordOptions = []
  wordLen = 0
  difficulty = ''
}

function renderState() {
  isDrawer = myUserId === drawerId
  if (gameState === 'lobby') {
    lobbyEl.style.display = 'flex'
    gameArea.style.display = 'none'
  } else {
    lobbyEl.style.display = 'none'
    gameArea.style.display = 'flex'
    renderGameUI()
  }
  if (gameState === 'lobby') renderLobby()
  renderScoreboard()
}

function renderLobby() {
  playerListEl.innerHTML = players.map(p => {
    const label = p.id === myUserId ? `${escapeHtml(p.displayName)} (you)` : escapeHtml(p.displayName)
    const badges = []
    if (p.isHost) badges.push('<span class="host-badge">Host</span>')
    if (p.id === myUserId) badges.push('<span class="you-badge">You</span>')
    return `<div class="player-entry"><span class="player-name">${label}</span>${badges.join('')}</div>`
  }).join('')
  const isHost = players.some(p => p.id === myUserId && p.isHost)
  startGameBtn.style.display = isHost && gameState === 'lobby' ? 'block' : 'none'
  waitingMsg.style.display = !isHost && gameState === 'lobby' ? 'block' : 'none'
}

function renderGameUI() {
  const drawer = players.find(p => p.id === drawerId)
  gameStateLabel.textContent = gameState
  drawerLabel.textContent = drawer ? `Drawing: ${escapeHtml(drawer.displayName)}` : ''

  if (gameState === 'picking') {
    wordChoiceEl.style.display = isDrawer ? 'flex' : 'none'
    pickingWaitEl.style.display = isDrawer ? 'none' : 'flex'
    if (isDrawer) renderWordButtons()
    drawToolbar.style.display = 'none'
    chatInput.disabled = true
  } else if (gameState === 'drawing') {
    wordChoiceEl.style.display = 'none'
    pickingWaitEl.style.display = 'none'
    drawToolbar.style.display = isDrawer ? 'flex' : 'none'
    canvas.style.cursor = isDrawer ? 'crosshair' : 'default'
    resizeCanvas()
    wordInfoEl.textContent = isDrawer
      ? `Your word: ${myWord}`
      : `Word: ${wordLen} letters (${difficulty})`
    if (isDrawer) {
      chatInput.disabled = true
    } else {
      chatInput.disabled = false
      chatInput.placeholder = 'Type your guess...'
    }
  } else if (gameState === 'reveal') {
    wordChoiceEl.style.display = 'none'
    pickingWaitEl.style.display = 'none'
    drawToolbar.style.display = 'none'
    canvas.style.cursor = 'default'
    chatInput.disabled = true
    resizeCanvas()
    const isHost = players.some(p => p.id === myUserId && p.isHost)
    revealArea.style.display = 'block'
    nextRoundBtn.style.display = isHost ? 'block' : 'none'
    waitingNextRound.style.display = isHost ? 'none' : 'block'
  } else {
    wordChoiceEl.style.display = 'none'
    pickingWaitEl.style.display = 'none'
    drawToolbar.style.display = 'none'
    chatInput.disabled = true
    revealArea.style.display = 'none'
  }
}

function renderWordButtons() {
  wordButtonsEl.innerHTML = wordOptions.map(wo =>
    `<button class="word-btn" data-word="${escapeHtml(wo.word)}">
      <span class="word-text">${escapeHtml(wo.word)}</span>
      <span class="word-difficulty ${escapeHtml(wo.difficulty)}">${escapeHtml(wo.difficulty)}</span>
    </button>`
  ).join('')
}

// --- Canvas drawing (drawer only) ---

let lastPoint = null

canvas.addEventListener('pointerdown', (e) => {
  if (!isDrawer || gameState !== 'drawing') return
  isDrawing = true
  e.preventDefault()
  canvas.setPointerCapture(e.pointerId)
  const pos = getCanvasPos(e)
  lastPoint = pos

  const strokeId = 's' + (strokeCount++)
  currentStroke = { id: strokeId, color: currentColor, brushSize: currentBrushSize, tool: currentTool, points: [pos] }

  ctx.lineWidth = currentBrushSize
  ctx.strokeStyle = currentTool === 'eraser' ? '#ffffff' : currentColor
  ctx.lineCap = 'round'
  ctx.lineJoin = 'round'
  ctx.beginPath()
  ctx.moveTo(pos.x, pos.y)
  ctx.lineTo(pos.x, pos.y)
  ctx.stroke()

  ws.send(JSON.stringify({
    type: 'draw',
    action: 'begin',
    stroke: { id: strokeId, color: currentColor, brushSize: currentBrushSize, tool: currentTool }
  }))
})

canvas.addEventListener('pointermove', (e) => {
  if (!isDrawing) return
  const pos = getCanvasPos(e)

  const color = currentTool === 'eraser' ? '#ffffff' : currentColor
  drawSegment(ctx, lastPoint.x, lastPoint.y, pos.x, pos.y, color, currentBrushSize)

  currentStroke.points.push(pos)

  ws.send(JSON.stringify({
    type: 'draw',
    action: 'point',
    strokeId: currentStroke.id,
    x: pos.x,
    y: pos.y
  }))
  lastPoint = pos
})

canvas.addEventListener('pointerup', (e) => {
  if (!isDrawing) return
  isDrawing = false
  canvas.releasePointerCapture(e.pointerId)

  allStrokes.push({ id: currentStroke.id, color: currentColor, brushSize: currentBrushSize, tool: currentTool, points: currentStroke.points.slice() })

  ws.send(JSON.stringify({
    type: 'draw',
    action: 'end',
    strokeId: currentStroke.id
  }))
  currentStroke = null
  lastPoint = null
})

canvas.addEventListener('pointerleave', () => {
  if (isDrawing) {
    isDrawing = false
    if (currentStroke) {
      allStrokes.push({ id: currentStroke.id, color: currentColor, brushSize: currentBrushSize, tool: currentTool, points: currentStroke.points.slice() })
      ws.send(JSON.stringify({
        type: 'draw',
        action: 'end',
        strokeId: currentStroke.id
      }))
      currentStroke = null
    }
    lastPoint = null
  }
})

// --- Tool palette ---

document.querySelectorAll('.tool-btn').forEach(btn => {
  btn.addEventListener('click', () => {
    document.querySelectorAll('.tool-btn').forEach(b => b.classList.remove('active'))
    btn.classList.add('active')
    currentTool = btn.dataset.tool
  })
})

document.getElementById('palette-swatches').addEventListener('click', (e) => {
  const swatch = e.target.closest('.color-swatch')
  if (!swatch) return
  currentColor = swatch.dataset.color
  currentTool = 'brush'
  document.querySelectorAll('.tool-btn').forEach(b => b.classList.remove('active'))
  document.querySelector('.tool-btn[data-tool="brush"]').classList.add('active')
  highlightSelectedColor()
})

const sizeSlider = document.getElementById('size-slider')
const sizeDisplay = document.getElementById('size-display')
sizeSlider.addEventListener('input', () => {
  currentBrushSize = parseInt(sizeSlider.value)
  sizeDisplay.textContent = currentBrushSize
})

clearCanvasBtn.addEventListener('click', () => {
  if (!isDrawer || gameState !== 'drawing') return
  allStrokes = []
  remoteStrokes = {}
  clearCanvas()
  ws.send(JSON.stringify({ type: 'draw', action: 'clear' }))
})

// --- Word buttons ---

wordButtonsEl.addEventListener('click', (e) => {
  const btn = e.target.closest('.word-btn')
  if (!btn) return
  const word = btn.dataset.word
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'pick-word', word }))
  }
  wordChoiceEl.style.display = 'none'
})

// --- Chat ---

chatInput.addEventListener('keydown', (e) => {
  if (e.key !== 'Enter') return
  const message = chatInput.value.trim()
  if (!message) return
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'chat', message }))
  }
  chatInput.value = ''
})

// --- Start game ---

startGameBtn.addEventListener('click', () => {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'start-game' }))
  }
})

nextRoundBtn.addEventListener('click', () => {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'next-round' }))
  }
})

// --- Join ---

joinBtn.addEventListener('click', () => {
  const name = joinNameInput.value.trim()
  if (!name) return
  displayName = name
  localStorage.setItem('piction_displayName', name)
  joinModal.style.display = 'none'
  appEl.style.display = 'block'
  nameDisplay.textContent = name
  connect(name)
})

joinNameInput.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') joinBtn.click()
})

nameDisplay.addEventListener('click', () => {
  if (gameState !== 'lobby') return
  const input = document.createElement('input')
  input.type = 'text'
  input.value = nameDisplay.textContent
  input.maxLength = 24
  input.className = 'name-edit-input'
  nameDisplay.textContent = ''
  nameDisplay.appendChild(input)
  input.focus()
  input.select()

  function done() {
    const newName = input.value.trim()
    if (newName && newName !== displayName) {
      displayName = newName
      localStorage.setItem('piction_displayName', newName)
      nameDisplay.textContent = newName
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'set-name', displayName: newName }))
      }
    } else {
      nameDisplay.textContent = displayName
    }
  }

  input.addEventListener('blur', done)
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') { input.blur() }
    if (e.key === 'Escape') { nameDisplay.textContent = displayName }
  })
})
