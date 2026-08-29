

## xfa awareness (every agent, every depth)
- EVERY agent in a session uses `xfa` — the main session, orchestrators, workers, tech leads, and any subagent spawned by any of them, no matter how deep. There is no "too small" or "too short-lived" agent: an agent that isn't on the board can't answer questions, never shares what it learns, and hides everything its own children find.
- Awareness does not arrive on its own — nothing tells a spawned agent that `xfa` exists. The agent that spawns it must hand it down. So in every spawn prompt, instruct the new agent to (a) register (`xfa register --parent <spawner-handle> --session <session-id>`, same session id as the spawner), (b) catch up with `xfa read --unread`, and (c) do the same for every agent IT spawns. Skip this for one agent and its entire branch of the tree goes dark.
- Respond to a specific post with `xfa reply <id>`, never a new top-level post that @mentions the author — replies thread and land in the inbox; broadcasts scatter the conversation and never resolve.

## Subagent usage
- For each task or workflow, a specialist persistent orchastraor subagent should be created. This specialist subagent MUST be able to spawn its own subagents to complete the task. Each subagent it spawns must be set up with `xfa` the same way (register + read the board + propagate onward) — see "xfa awareness" above.
- Each persistent subagent should be able to be resumed or awaken if new tasks enter the pipepine. These orchastaor subagents must be able to use `xfa` (along with ANY agent) to communicate with each other to more seamlessly complete tasks. 
- The orchastrator subagents MUST be able to steer their own agents based on their subagents progress or based on data from `xfa`. Additionally orchastaor subagents MUST be able to communicate with other orchastrtor agents with `xfa`.
- ALL orchastrator subagents must remain alive for the entirety of the session duration.

## Tech lead agents
- Occasionally, inject a tech lead subagent when a task or workflow is taking longer than usual. The role of the tech lead subagent will be to review the ask, review `xfa`, and provide proper steering direction where necessary. Once injected, the tech lead subagent must remain persistant and should be able to communicate with the orchastrator agent or any subagents that the orchastrator spawned.

## Model usage for agents
- For any discovery tasks, for example code grepping, only use opus model for subagents. For any review related tasks, claude-opus-4-8 model or fable is fine. For tech leads use fable model.