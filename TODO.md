I am currently reviewing the report for errors and problems that may appear.

Current notes:
- need to remove all em-dashes from the report.
- need to remove the use of semi-colons (;) from the middle of the text.
- section 1.1 (key contributions) do not make sense. I built the system from the ground, so what the fuck is even that.
- ensure that links such as references show up as black and not green.
- when mentioning other sections, currently the link is only in the number and the number is blue. I would prefer if the link was in both the number and the "Section" word and for it to be black. For example, clicking on "Section 2" would take to section 2, no need to only click on the number "2".
- ensure that all paragraphs start at the begining of the line and don't have "tabs".
- in figure 1, the boxes with the prometeus server and the wazuh manager have text that is overlapping. Some of that small text that should be appearing outside is, in fact, inside and overalapping the rest of the text. Additionally, the Health Exporter box seems to be fully overlapping the Ingestion Gateway. Same with the Prometeus server being overlaping in it's entirety the Diagnostics box. Please ensure that there are no overlappings.
- change mentions of "we" to "I". This is an individual project.
- figure 2 has some of the boxes overlaping each other, please ensure that this does not happen.
- when mentioning requirements (or anything for that matter), do not mention specific slides in the guidelines, there is no need for that.


Possible improvements:
- add an additional threat to prevent. SQL injection seems an interesting on, that could potentially easily be done (thanks to Wazuh). Need to look into this. Because if we don't even take in parameters in any of the databases, then there is no possibility of SQL injection in the first place.
