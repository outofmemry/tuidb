// Saved chat helpers.


// chat title: the first user message cut to 60 characters, like store.Chat
export function chatTitle(msgs: { role: string; content: string }[]): string {
  const m = msgs.find((x) => x.role === 'user')?.content.trim().replace(/\s+/g, ' ') ?? ''
  const a = Array.from(m)
  return a.length > 60 ? a.slice(0, 60).join('') : m
}
