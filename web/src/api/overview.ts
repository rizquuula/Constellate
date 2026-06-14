import { wsBaseURL } from './ws'

export function openOverviewSocket(): WebSocket {
  const url = `${wsBaseURL()}/ws/overview`
  const ws = new WebSocket(url)
  ws.binaryType = 'arraybuffer'
  return ws
}
