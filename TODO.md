I am currently reviewing the report for errors and problems that may appear.

Current notes:
- [x] need to remove all em-dashes from the report.
- [x] need to remove the use of semi-colons (;) from the middle of the text.
- [x] section 1.1 (key contributions) do not make sense. I built the system from the ground, so what the fuck is even that.
- [x] ensure that links such as references show up as black and not green.
- [x] when mentioning other sections, currently the link is only in the number and the number is blue. I would prefer if the link was in both the number and the "Section" word and for it to be black. For example, clicking on "Section 2" would take to section 2, no need to only click on the number "2". This should be the same for Tables and Figures.
- [x] ensure that all paragraphs start at the begining of the line and don't have "tabs".
- [x] in figure 1, the boxes with the prometeus server and the wazuh manager have text that is overlapping. Some of that small text that should be appearing outside is, in fact, inside and overalapping the rest of the text. Additionally, the Health Exporter box seems to be fully overlapping the Ingestion Gateway. Same with the Prometeus server being overlaping in it's entirety the Diagnostics box. Please ensure that there are no overlappings.
- [x] change mentions of "we" to "I". This is an individual project.
- [x] figure 2 has some of the boxes overlaping each other, please ensure that this does not happen.
- [x] when mentioning requirements (or anything for that matter), do not mention specific slides in the guidelines, there is no need for that.
- [x] Please ensure that tables and figures appear exactly where they are supposed, even if space needs to be left open. Also, ensure that all tables and figures are mentioned in the text (the previous text needs to mention them).
- [x] ensure that formated text (for example: <if_matched_group>web</if_matched_gro) doesn't continue after the page borders and goes off page.
- [x] Table 3 is going off the page. Ensure that it does not go over border. Same problem for Table 4 and 5.
- [x] Remove mentions of project 2. I don't even know what that is going to be, so no need to mention it. Instead of mentioning project 2, mention future work. No need to mention project 2 specifically.
- [x] mention the limitations of the solution for the Man in the middle attacks. Specifically towards problems related to clock drift in the sensor. Mention, that a valid sensor might just have a clock drift and end up being blocked (if their internal clock has a drift bigger than 10 seconds). And despite this being something that can happen, we accepted this downside, since it would protect us from other attacks.
- [x] generalize ingestion's DoS prevention logic (token-bucket rate limiting per IP and payload size clamping) into shared middleware and adopt across all microservices (registry, storage, analytics, temperature, and exporters).
- [x] Tier 2 Global SIEM Quarantine with Wazuh: configure all microservices to ship Apache access logs to Wazuh, add Wazuh Rules 100105 (repeated 429 rate-limit violations) and 100106 (repeated 413 oversized submissions), and configure active response with <location>all</location> to drop offending IPs across all microservices.
- [x] Hardened container firewalls (iptables) and Wazuh agents across all microservice containers in deploy.sh (ssle-ingestion, ssle-registry, ssle-storage, ssle-analytics).


Possible improvements:
- add an additional threat to prevent. SQL injection seems an interesting on, that could potentially easily be done (thanks to Wazuh). Need to look into this. Because if we don't even take in parameters in any of the databases, then there is no possibility of SQL injection in the first place.
