// NameForge Front-end Logic

document.addEventListener("DOMContentLoaded", () => {
    // 1. Initial State & Configuration
    let results = [];
    let currentFilter = "all";

    // Selectors
    const form = document.getElementById("generator-form");
    const submitBtn = document.getElementById("submit-btn");
    const loaderSpinner = submitBtn.querySelector(".loader-spinner");
    const btnContent = submitBtn.querySelector(".btn-content");

    const loadingArea = document.getElementById("loading-area");
    const loadingStep = document.getElementById("loading-step");
    const progressBar = document.getElementById("progress-bar");

    const resultsArea = document.getElementById("results-area");
    const resultsGrid = document.getElementById("results-grid");
    const resultsCountBadge = document.getElementById("results-count-badge");

    // Metrics Selectors
    const metricLatency = document.getElementById("metric-latency");
    const metricAvailable = document.getElementById("metric-available");
    const metricTotal = document.getElementById("metric-total");

    // Tag Handlers (Vibe/Themes)
    document.querySelectorAll(".tag-selector").forEach(selector => {
        selector.addEventListener("click", (e) => {
            if (e.target.classList.contains("tag")) {
                e.target.classList.toggle("active");
            }
        });
    });

    // Filter Buttons Handlers
    document.querySelectorAll(".filter-btn").forEach(btn => {
        btn.addEventListener("click", (e) => {
            document.querySelectorAll(".filter-btn").forEach(b => b.classList.remove("active"));
            e.target.classList.add("active");
            currentFilter = e.target.getAttribute("data-filter");
            renderResults();
        });
    });

    // 2. Health Check & Diagnostics
    async function checkHealth() {
        try {
            const resp = await fetch("/health");
            const data = await resp.json();
            
            const dbVal = document.querySelector("#status-db .status-val");
            const cacheVal = document.querySelector("#status-cache .status-val");

            if (data.postgres === "connected") {
                dbVal.textContent = "Online";
                dbVal.className = "status-val text-green";
            } else {
                dbVal.textContent = "Offline (Local Only)";
                dbVal.className = "status-val text-red";
            }

            if (data.redis === "connected") {
                cacheVal.textContent = "Active";
                cacheVal.className = "status-val text-green";
            } else {
                cacheVal.textContent = "Disabled";
                cacheVal.className = "status-val text-dim";
            }
        } catch (err) {
            console.error("Failed to perform health check:", err);
        }
    }
    
    // Boot diagnostics
    checkHealth();

    // 3. Search Submit Orchestrator
    form.addEventListener("submit", async (e) => {
        e.preventDefault();

        // Retrieve values
        const description = document.getElementById("description").value;
        const avoidInput = document.getElementById("avoid").value;

        // Vibe/Style tags
        const styles = Array.from(document.querySelectorAll("#style-tags .tag.active"))
            .map(el => el.getAttribute("data-value"));
        
        // Theme tags
        const themes = Array.from(document.querySelectorAll("#theme-tags .tag.active"))
            .map(el => el.getAttribute("data-value"));

        // TLD Checkboxes
        const tlds = Array.from(document.querySelectorAll("input[name='tlds']:checked"))
            .map(el => el.value);

        const avoid = avoidInput.split(",")
            .map(s => s.trim())
            .filter(s => s !== "");

        if (tlds.length === 0) {
            alert("Please select at least one TLD.");
            return;
        }

        // Toggle UI loading states
        submitBtn.disabled = true;
        loaderSpinner.classList.remove("hidden");
        btnContent.classList.add("hidden");
        resultsArea.classList.add("hidden");
        loadingArea.classList.remove("hidden");

        // Progress milestones simulator
        const progressSteps = [
            { width: 10, text: "Spinning worker pools..." },
            { width: 25, text: "Running AI, Morphological, and Hybrid generators..." },
            { width: 50, text: "Filtering phonetics and consonants anomalies..." },
            { width: 70, text: "Checking Redis cache & querying Porkbun/Namecheap..." },
            { width: 85, text: "Pricing availability results..." },
            { width: 95, text: "Ranking brands with scoring model..." }
        ];

        let currentStepIndex = 0;
        progressBar.style.width = "0%";
        loadingStep.textContent = "Initializing backend pipelines...";

        const interval = setInterval(() => {
            if (currentStepIndex < progressSteps.length) {
                const step = progressSteps[currentStepIndex];
                progressBar.style.width = `${step.width}%`;
                loadingStep.textContent = step.text;
                currentStepIndex++;
            }
        }, 500);

        const startTime = performance.now();

        try {
            const response = await fetch("/generate", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ description, style: styles, themes, tlds, avoid })
            });

            if (!response.ok) {
                throw new Error("Naming engine returned error");
            }

            const data = await response.json();
            results = data.results || [];
            
            // End milestones simulation and complete progress bar
            clearInterval(interval);
            progressBar.style.width = "100%";
            loadingStep.textContent = "Finalizing results...";

            setTimeout(() => {
                const latency = Math.round(performance.now() - startTime);
                
                // Set metrics display
                metricLatency.textContent = `${latency}ms`;
                
                const availableCount = results.filter(r => r.available).length;
                metricAvailable.textContent = availableCount;
                metricTotal.textContent = results.length;

                resultsCountBadge.textContent = `${results.length} suggestions`;

                // Display Results area
                loadingArea.classList.add("hidden");
                resultsArea.classList.remove("hidden");
                
                // Reset submit button state
                submitBtn.disabled = false;
                loaderSpinner.classList.add("hidden");
                btnContent.classList.remove("hidden");

                renderResults();
            }, 300);

        } catch (error) {
            clearInterval(interval);
            alert("Error: Failed to fetch results from naming engine. Ensure the server is online.");
            submitBtn.disabled = false;
            loaderSpinner.classList.add("hidden");
            btnContent.classList.remove("hidden");
            loadingArea.classList.add("hidden");
            console.error("Fetch error:", error);
        }
    });

    // 4. Render Results Card Listing
    function renderResults() {
        resultsGrid.innerHTML = "";

        // Filter items
        let filteredResults = results;
        if (currentFilter === "available") {
            filteredResults = results.filter(r => r.available);
        } else if (currentFilter === "premium") {
            filteredResults = results.filter(r => r.score >= 80);
        }

        if (filteredResults.length === 0) {
            resultsGrid.innerHTML = `
                <div class="glass-card" style="grid-column: 1 / -1; padding: 40px; text-align: center; color: var(--text-secondary);">
                    <i class="fa-solid fa-folder-open" style="font-size: 2rem; margin-bottom: 12px; color: var(--text-muted);"></i>
                    <p>No results match the selected filter.</p>
                </div>
            `;
            return;
        }

        // Group by Name
        const groupedResults = {};
        filteredResults.forEach(item => {
            if (!groupedResults[item.name]) {
                groupedResults[item.name] = {
                    name: item.name,
                    score: item.score,
                    domains: []
                };
            }
            groupedResults[item.name].domains.push(item);
        });

        // Now render grouped cards
        Object.values(groupedResults).forEach(group => {
            const card = document.createElement("div");
            card.className = "result-card";
            card.style.flexDirection = "column";
            card.style.alignItems = "stretch";

            const scoreClass = group.score >= 80 ? "score-badge high" : "score-badge";
            
            // Helper for registration URL based on winner platform
            function getRegisterUrl(platform, domain) {
                switch (platform.toLowerCase()) {
                    case 'godaddy':
                        return `https://in.godaddy.com/domainfind/search?q=${domain}`;
                    case 'hostinger':
                        return `https://hostinger.in/domain-name-search?domain=${domain}`;
                    case 'bigrock':
                        return `https://www.bigrock.in/domain-registration/search.php?domain=${domain}`;
                    case 'namecheap':
                        return `https://www.namecheap.com/domains/registration/results/?domain=${domain}`;
                    case 'porkbun':
                    default:
                        return `https://porkbun.com/checkout/search?q=${domain}`;
                }
            }

            // Build inner HTML for the domains list
            let domainsHtml = '<div class="domain-list" style="margin-top: 15px; display: flex; flex-direction: column; gap: 12px;">';
            group.domains.forEach(d => {
                const availabilityClass = d.available ? "availability-badge available" : "availability-badge taken";
                const availabilityText = d.available ? '<i class="fa-solid fa-check"></i>' : '<i class="fa-solid fa-xmark"></i>';
                const priceSymbol = d.currency === "INR" ? "₹" : "$";
                const priceText = d.available ? `${priceSymbol}${d.price.toFixed(2)}` : "N/A";
                const registerDisabled = d.available ? "" : "disabled";
                const platformBadge = d.platform ? `<span class="platform-badge" style="font-size: 0.65rem; padding: 2px 6px; margin-left: 6px; background: var(--neon-indigo); color: white; border-radius: 4px;"><i class="fa-solid fa-star"></i> Best: ${d.platform}</span>` : "";

                // Build pricing offers table
                let offersHtml = '';
                if (d.available && d.offers && d.offers.length > 0) {
                    offersHtml = `
                        <div class="price-comparison" style="margin-top: 8px; padding: 8px 12px; background: rgba(0,0,0,0.01); border-radius: 6px; border-left: 3px solid var(--neon-indigo); display: flex; flex-direction: column; gap: 6px; font-size: 0.85rem;">
                            <div style="font-weight: 600; color: var(--text-secondary); margin-bottom: 2px; display: flex; justify-content: space-between; align-items: center;">
                                <span>Registrar Comparison (INR ₹)</span>
                                <span style="font-size: 0.75rem; color: var(--neon-indigo); font-weight: normal;"><i class="fa-solid fa-circle-info"></i> Rates compared in real-time</span>
                            </div>
                            <div style="display: grid; grid-template-columns: repeat(auto-fill, minmax(130px, 1fr)); gap: 8px;">
                    `;

                    d.offers.forEach(o => {
                        const isBestBadge = o.is_best ? '<span class="best-deal-badge" style="font-size: 0.65rem; background: var(--neon-green); color: white; padding: 2px 6px; border-radius: 4px; font-weight: 700; margin-left: 4px;">Best Deal</span>' : '';
                        const offerStyle = o.is_best ? 'border: 1px solid var(--neon-green); background: rgba(5, 150, 105, 0.05);' : 'border: 1px solid var(--border-color); background: rgba(255, 255, 255, 0.6);';
                        
                        offersHtml += `
                                <div style="padding: 6px 8px; border-radius: 6px; ${offerStyle} display: flex; flex-direction: column; justify-content: space-between;">
                                    <span style="font-weight: 600; color: var(--text-primary); display: flex; align-items: center; justify-content: space-between;">
                                        ${o.platform}
                                        ${isBestBadge}
                                    </span>
                                    <span style="font-weight: 700; color: var(--text-primary); margin-top: 4px; font-size: 0.9rem;">₹${o.price.toFixed(2)}</span>
                                </div>
                        `;
                    });

                    offersHtml += `
                            </div>
                        </div>
                    `;
                }

                domainsHtml += `
                    <div class="domain-container" style="display: flex; flex-direction: column; background: rgba(0,0,0,0.015); border-radius: 8px; padding: 10px; border: 1px solid var(--border-color);">
                        <div class="domain-row" style="display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 8px;">
                            <div style="display: flex; align-items: center; gap: 8px; flex-wrap: wrap;">
                                <span class="result-domain" style="font-weight: 600; font-size: 1.1rem; color: var(--text-primary);">${d.domain}</span>
                                <span class="${availabilityClass}" style="transform: scale(0.9);">${availabilityText}</span>
                                ${platformBadge}
                            </div>
                            <div style="display: flex; align-items: center; gap: 10px;">
                                <span class="price-tag" style="font-weight: 700; color: var(--text-primary); font-size: 1.1rem;">${priceText}</span>
                                <button class="btn-action btn-copy" style="width: 32px; height: 32px;" title="Copy Domain" data-domain="${d.domain}">
                                    <i class="fa-regular fa-copy"></i>
                                </button>
                                <button class="btn-register" style="padding: 6px 12px; font-size: 0.75rem;" ${registerDisabled} data-platform="${d.platform}" data-domain="${d.domain}">
                                    Get
                                </button>
                            </div>
                        </div>
                        ${offersHtml}
                    </div>
                `;
            });
            domainsHtml += '</div>';

            card.innerHTML = `
                <div class="result-main" style="width: 100%;">
                    <div class="result-name-row" style="justify-content: space-between;">
                        <span class="result-name" style="font-size: 1.8rem;">${group.name}</span>
                        <span class="${scoreClass}">Brand Score: ${group.score}</span>
                    </div>
                </div>
                ${domainsHtml}
            `;

            // Bind copy button listener for all copy buttons in this card
            card.querySelectorAll(".btn-copy").forEach(btn => {
                btn.addEventListener("click", (e) => {
                    const button = e.currentTarget;
                    const domain = button.getAttribute("data-domain");
                    navigator.clipboard.writeText(domain).then(() => {
                        const icon = button.querySelector("i");
                        icon.className = "fa-solid fa-check text-green";
                        button.style.borderColor = "var(--neon-green)";
                        setTimeout(() => {
                            icon.className = "fa-regular fa-copy";
                            button.style.borderColor = "";
                        }, 2000);
                    });
                });
            });

            // Bind register button listener for all register buttons in this card
            card.querySelectorAll(".btn-register").forEach(btn => {
                btn.addEventListener("click", (e) => {
                    const button = e.currentTarget;
                    const platform = button.getAttribute("data-platform");
                    const domain = button.getAttribute("data-domain");
                    window.open(getRegisterUrl(platform, domain), '_blank');
                });
            });

            resultsGrid.appendChild(card);
        });
    }
});
