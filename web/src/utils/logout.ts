// The steps of logging out, with the browser and the API passed in so they can be tested (utils/api.ts wires the real ones).
export type LogoutDeps = {
  sessions: string[]                                  // the session ids this browser tab opened
  call: (method: string, path: string) => Promise<unknown>
  forget: () => void                                  // drop the token and everything else this tab remembered
  go: () => void                                      // back to the start page, which asks to sign in again
}

// logoutSteps closes the tab's sessions (no database connection is left behind), asks the server to expire the [web] password login
// cookie, forgets the token and goes to the start. A step that fails (the server is gone) never stops the later ones.
export async function logoutSteps(d: LogoutDeps) {
  await Promise.all(d.sessions.map((id) => d.call('DELETE', `/sessions/${id}`).catch(() => {})))
  await d.call('POST', '/logout').catch(() => {})
  d.forget()
  d.go()
}
