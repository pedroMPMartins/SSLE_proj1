I am currently reviewing the report for errors and problems that may appear.

Current notes:
- [x] need to remove all em-dashes from the report.
- [x] need to remove the use of semi-colons (;) from the middle of the text.
- section 1.1 (key contributions) do not make sense. I built the system from the ground, so what the fuck is even that.
- ensure that links such as references show up as black and not green.
- when mentioning other sections, currently the link is only in the number and the number is blue. I would prefer if the link was in both the number and the "Section" word and for it to be black. For example, clicking on "Section 2" would take to section 2, no need to only click on the number "2". This should be the same for Tables and Figures.
- ensure that all paragraphs start at the begining of the line and don't have "tabs".
- in figure 1, the boxes with the prometeus server and the wazuh manager have text that is overlapping. Some of that small text that should be appearing outside is, in fact, inside and overalapping the rest of the text. Additionally, the Health Exporter box seems to be fully overlapping the Ingestion Gateway. Same with the Prometeus server being overlaping in it's entirety the Diagnostics box. Please ensure that there are no overlappings.
- change mentions of "we" to "I". This is an individual project.
- figure 2 has some of the boxes overlaping each other, please ensure that this does not happen.
- when mentioning requirements (or anything for that matter), do not mention specific slides in the guidelines, there is no need for that.
- Please ensure that tables and figures appear exactly where they are supposed, even if space needs to be left open. Also, ensure that all tables and figures are mentioned in the text (the previous text needs to mention them).
- ensure that formated text (for example: <if_matched_group>web</if_matched_gro) doesn't continue after the page borders and goes off page.
- Table 3 is going off the page. Ensure that it does not go over border. Same problem for Table 4 and 5.
- Remove mentions of project 2. I don't even know what that is going to be, so no need to mention it. Instead of mentioning project 2, mention future work. No need to mention project 2 specifically.


Possible improvements:
- add an additional threat to prevent. SQL injection seems an interesting on, that could potentially easily be done (thanks to Wazuh). Need to look into this. Because if we don't even take in parameters in any of the databases, then there is no possibility of SQL injection in the first place.
- currently, rate limiting is only implemented in the ingestion service. It could be interesting for a global system that would drop ips when they go over the amount of requests permitted. Check if the Wazuh has that functionality and implement, so all microservices are actually protected. Same with the payload size.
- currently, only the ingestion service seems to be correctly protected with firewalls and whatnot. While it is true that an internet attacker cannot reach other microservices since they are comunicating over private networks when deployed with `deploy.sh`, this is unacceptable, since we are trying to mimick actual real environments. Implementation wize, it is currently pretty good, but we need to have all of the services protected (other than the sensors, those are probably not as important).
- mention the limitations of the solution for the Man in the middle attacks. Specifically towards problems related to clock drift in the sensor. Mention, that a valid sensor might just have a clock drift and end up being blocked (if their internal clock has a drift bigger than 10 seconds). And despite this being something that can happen, we accepted this downside, since it would protect us from other attacks.
