import { create } from 'zustand'
import type { Machine, Session } from '../types'
import { listMachines, listSessions, createSession } from '../api/rest'

interface Store {
  machines: Machine[]
  sessions: Session[]
  activeSessionId: string | null
  refreshMachines: () => Promise<void>
  refreshSessions: () => Promise<void>
  openShell: (machineID: string, cols: number, rows: number) => Promise<void>
  setActive: (id: string) => void
}

export const useStore = create<Store>((set) => ({
  machines: [],
  sessions: [],
  activeSessionId: null,

  refreshMachines: async () => {
    const machines = await listMachines()
    set({ machines })
  },

  refreshSessions: async () => {
    const sessions = await listSessions()
    set({ sessions })
  },

  openShell: async (machineID, cols, rows) => {
    const session = await createSession(machineID, cols, rows)
    const sessions = await listSessions()
    set({ sessions, activeSessionId: session.id })
  },

  setActive: (id) => set({ activeSessionId: id }),
}))
