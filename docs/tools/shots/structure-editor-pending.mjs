import { connect, openStructure, pendingEdits } from './lib.mjs'
export default async (page) => { await connect(page); await openStructure(page); await pendingEdits(page) }
