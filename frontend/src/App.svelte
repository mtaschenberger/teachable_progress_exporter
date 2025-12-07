<script lang="ts">
  import { onMount } from "svelte";
  import { GetDefaultPath, RunExport } from "../wailsjs/go/main/App";
  import { EventsOn } from "../wailsjs/runtime/runtime";

  let courseID = "";
  let apiToken = "";
  let outputPath = "";
  let isRunning = false;
  let errorMessage: string | null = null;
  let successMessage: string | null = null;
  let lastFilePath: string | null = null;

  // logging state
  let logs: string[] = [];
  let currentStatus = "";

  $: canRun =
    courseID.trim().length > 0 &&
    apiToken.trim().length > 0 &&
    outputPath.trim().length > 0;

  $: disabledInput = isRunning;

  onMount(async () => {
    try {
      outputPath = await GetDefaultPath();
    } catch (err) {
      console.error("Failed to get default path", err);
    }

    // subscribe to backend progress events
    EventsOn("export:progress", (data: any) => {
      let msg = "";
      if (typeof data === "string") {
        msg = data;
      } else if (data && typeof data === "object") {
        const step = data.step ?? "";
        const message = data.message ?? "";
        msg = step ? `[${step}] ${message}` : String(message);
      } else {
        msg = String(data);
      }

      logs = [...logs, msg];
      currentStatus = msg;
    });
  });

  async function handleSubmit(event?: Event) {
    if (event) event.preventDefault();

    errorMessage = null;
    successMessage = null;
    logs = [];
    currentStatus = "";

    if (!canRun) {
      errorMessage = "Please fill in Course ID, API token and output path.";
      return;
    }

    isRunning = true;
    try {
      const filePath = await RunExport(
        courseID.trim(),
        apiToken.trim(),
        outputPath.trim()
      );
      lastFilePath = filePath;
      successMessage = `Export completed.\nFile saved at:\n${filePath}`;
    } catch (err: any) {
      errorMessage = err?.message || String(err);
    } finally {
      isRunning = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === "Enter" && canRun && !isRunning) {
      handleSubmit(e);
    }
  }
</script>


<main class="page">
  <div class="shell">
    <header class="header">
      <h1>Course Exporter</h1>
      <p class="subtitle">
        Enter a Course ID, your API token, and the output folder.
        The export runs in the background and creates a CSV file.
      </p>
    </header>

    <section class="card">
      <form class="form" on:submit|preventDefault={handleSubmit}>
        <div class="field">
          <label for="courseID">Course ID</label>
          <input
            id="courseID"
            type="text"
            placeholder="e.g. 2843568"
            bind:value={courseID}
            on:keydown={handleKeydown}
            disabled={disabledInput}
          />
        </div>

        <div class="field">
          <label for="apiToken">API token</label>
          <input
            id="apiToken"
            type="password"
            placeholder="Your API token"
            bind:value={apiToken}
            on:keydown={handleKeydown}
            disabled={disabledInput}
          />
        </div>

        <div class="field">
          <label for="outputPath">Output path</label>
          <input
            id="outputPath"
            type="text"
            placeholder="Directory to save CSV"
            bind:value={outputPath}
            on:keydown={handleKeydown}
            disabled={disabledInput}
          />
        </div>

        <div class="actions">
          <button type="submit" disabled={!canRun || isRunning}>
            {#if isRunning}
              Running export…
            {:else}
              Run export
            {/if}
          </button>
        </div>
      </form>

      {#if errorMessage}
        <div class="message error">
          <pre>{errorMessage}</pre>
        </div>
      {/if}

      {#if successMessage}
        <div class="message success">
          <pre>{successMessage}</pre>
        </div>
      {/if}

      {#if lastFilePath}
        <div class="last">
          Last file:
          <code>{lastFilePath}</code>
        </div>
      {/if}
    </section>
    <section class="log-panel">
  <div class="log-header">
    <h2>Activity</h2>
    {#if currentStatus}
      <span class="log-status">Current: {currentStatus}</span>
    {/if}
  </div>
  {#if logs.length === 0}
    <p class="log-empty">No activity yet. Run an export to see details.</p>
  {:else}
    <ul class="log-list">
      {#each logs as line, i}
        <li>{line}</li>
      {/each}
    </ul>
  {/if}
</section>

  </div>
</main>

<style>
  :global(body) {
    margin: 0;
  }

  .page {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    background: radial-gradient(circle at top, #0b1120, #020617);
    color: #e5e7eb;
    font-family: system-ui, sans-serif;
  }

  .shell {
    width: 100%;
    max-width: 700px;
    padding: 2rem;
    box-sizing: border-box;
  }

  .header h1 {
    margin-bottom: 0.5rem;
    font-size: 1.8rem;
  }

  .subtitle {
    margin-bottom: 1.5rem;
    color: #9ca3af;
  }

  .card {
    padding: 1.75rem;
    border-radius: 1rem;
    background: rgba(15, 23, 42, 0.95);
    border: 1px solid rgba(55, 65, 81, 0.8);
    box-shadow: 0 18px 45px rgba(0, 0, 0, 0.5);
  }

  .form {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .field label {
    font-size: 0.9rem;
    font-weight: 600;
  }

  .field input {
    padding: 0.55rem 0.75rem;
    font-size: 0.95rem;
    border-radius: 0.6rem;
    background: #020617;
    border: 1px solid #374151;
    color: #f3f4f6;
  }

  .actions {
    margin-top: 0.5rem;
  }

  button {
    padding: 0.65rem 1.4rem;
    border: none;
    border-radius: 999px;
    font-weight: 600;
    color: white;
    background: linear-gradient(135deg, #3b82f6, #22c55e);
    cursor: pointer;
  }

  button:disabled {
    opacity: 0.6;
    cursor: default;
  }

  .message {
    margin-top: 1rem;
    padding: 0.8rem 1rem;
    border-radius: 0.8rem;
    font-size: 0.85rem;
    white-space: pre-wrap;
  }

  .message.error {
    border: 1px solid rgba(248, 113, 113, 0.8);
    background: rgba(127, 29, 29, 0.25);
    color: #fecaca;
  }

  .message.success {
    border: 1px solid rgba(22, 163, 74, 0.85);
    background: rgba(6, 46, 22, 0.5);
    color: #bbf7d0;
  }

  .last {
    margin-top: 0.75rem;
    font-size: 0.85rem;
    color: #9ca3af;
  }

  code {
    display: block;
    margin-top: 0.25rem;
    padding: 0.3rem 0.4rem;
    border-radius: 0.4rem;
    background: rgba(15, 23, 42, 0.9);
    border: 1px solid rgba(55, 65, 81, 0.9);
  }
    .log-panel {
    margin-top: 1.5rem;
    padding-top: 1rem;
    border-top: 1px solid rgba(55, 65, 81, 0.8);
  }

  .log-header {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: 0.5rem;
  }

  .log-header h2 {
    margin: 0;
    font-size: 0.95rem;
    font-weight: 600;
    color: #e5e7eb;
  }

  .log-status {
    font-size: 0.8rem;
    color: #9ca3af;
  }

  .log-empty {
    font-size: 0.8rem;
    color: #6b7280;
  }

  .log-list {
    list-style: none;
    padding: 0;
    margin: 0;
    max-height: 180px;
    overflow-y: auto;
    font-size: 0.8rem;
    line-height: 1.4;
  }

  .log-list li {
    padding: 0.15rem 0;
    color: #9ca3af;
  }

</style>
