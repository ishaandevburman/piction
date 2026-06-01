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

const pathParts = window.location.pathname.split('/').filter(Boolean)
let roomId = (pathParts[0] === 'room' && pathParts[1]) ? pathParts[1] : 'default'
roomId = roomId.replace(/[^a-zA-Z0-9_-]/g, '_')
roomLabelEl.textContent = roomId === 'default' ? '' : roomId
joinRoomLabel.textContent = roomId === 'default' ? '' : 'Room: ' + roomId

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

joinModal.style.display = 'flex'
joinNameInput.focus()

function escapeHtml(s) {
  const d = document.createElement('div')
  d.textContent = s
  return d.innerHTML
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
    ws.send(JSON.stringify({
      type: 'join',
      userId: userId,
      displayName: name,
    }))
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
        renderLobby()
        break
      case 'players':
        players = msg.players || []
        renderLobby()
        break
      case 'game-state':
        gameState = msg.state
        drawerId = msg.drawerId || ''
        renderLobby()
        break
    }
  }
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

startGameBtn.addEventListener('click', () => {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ type: 'start-game' }))
  }
})

joinBtn.addEventListener('click', () => {
  const name = joinNameInput.value.trim()
  if (!name) return
  displayName = name
  localStorage.setItem('piction_displayName', name)
  joinModal.style.display = 'none'
  appEl.style.display = 'block'
  connect(name)
})

joinNameInput.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') joinBtn.click()
})

nameDisplay.addEventListener('click', () => {
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
