export const campaignImage = "/images/launch-demo/fieldwork-retreat.webp";

export const launchTasks = [
  { id: "page", title: "Review the landing page and signup flow", owner: "Omar", due: "Day 1" },
  { id: "design", title: "Approve the campaign artwork", owner: "Maya", due: "Day 1" },
  { id: "announce", title: "Schedule the launch announcement", owner: "Leah", due: "Day 2" },
  { id: "publish", title: "Connect the domain and publish", owner: "Omar", due: "Day 3" },
];

export const campaignCopy = "Make room for your next idea.\n\nIntroducing Fieldwork: three days by the sea for people who make things. A little distance from the everyday. A lot of room for what comes next.\n\nJoin the list for our first creative retreat.";

const pageCss = [
  "*{box-sizing:border-box}body{margin:0;background:#f4f0e6;color:#263b30;font-family:Arial,sans-serif}button,input{font:inherit}button{cursor:pointer}button:focus-visible,input:focus-visible{outline:2px solid #b65e3e;outline-offset:3px}",
  "header{display:flex;align-items:center;justify-content:space-between;padding:17px 28px}header strong{font-family:Georgia,serif;font-size:23px;font-weight:400;letter-spacing:-1px}header span{max-width:160px;text-align:right;line-height:1.5;font-size:14px;color:#687166}.hero{display:grid;grid-template-columns:1fr 1fr;min-height:285px;margin:0 10px;background:#ebe5d6}",
  ".copy{padding:24px 27px 25px}.eyebrow{font-size:14px;color:#657060;margin:0 0 13px}h1{font-family:Georgia,serif;font-size:45px;font-weight:400;letter-spacing:-2px;line-height:1.02;margin:0 0 15px}h1 em{font-weight:400;color:#9a5238}.intro{font-size:16px;line-height:1.7;max-width:260px;margin:0 0 17px;color:#596557}.photo{width:100%;height:100%;min-height:285px;object-fit:cover;object-position:58% center}",
  ".join{background:#2d4434;color:#fff9e9;border:0;border-radius:4px;padding:12px 16px;font-size:14px}.join span{margin-left:20px}footer{display:grid;grid-template-columns:repeat(3,1fr);gap:20px;padding:19px 27px 22px}footer strong{display:block;font-family:Georgia,serif;font-size:16px;font-weight:400}footer span{display:block;font-size:14px;margin-top:5px;color:#687166}",
  "form{display:flex;gap:6px;flex-wrap:wrap;margin-top:12px}form[hidden]{display:none}label{width:100%;font-size:14px}input{flex:1;min-width:90px;width:100%;border:0;padding:11px;border-radius:4px;background:#fffdf6;font-size:16px}form button{background:#2d4434;color:white;border:0;border-radius:4px;padding:10px 13px;font-size:14px}.note{font-size:14px;line-height:1.5;color:#657060;width:100%;margin:1px 0 0}.thanks{font-size:16px;line-height:1.6;max-width:250px}.thanks[hidden]{display:none}",
  "@media(min-width:1000px) and (min-height:600px){header,footer{padding-inline:6vw}.hero{margin-inline:3vw;min-height:68vh}.copy{align-self:center;padding:5vw}h1{font-size:clamp(50px,6vw,90px)}.intro{font-size:16px;max-width:380px}.eyebrow{font-size:14px}.photo{min-height:68vh}.join{font-size:14px}}",
  "@media(max-width:560px){header{padding:13px 20px}header strong{font-size:21px}header span{max-width:160px;text-align:right;line-height:1.5;font-size:14px}.hero{grid-template-columns:1fr;margin:0 8px}.photo{grid-row:1;height:110px;min-height:0;object-position:center 58%}.copy{padding:18px}h1{font-size:32px;letter-spacing:-1.2px;max-width:280px;margin-bottom:10px}.eyebrow{margin-bottom:8px}.intro{font-size:16px;line-height:1.6;max-width:310px;margin-bottom:13px}.join{font-size:14px;padding:10px 14px}input{font-size:16px}footer{padding:18px 22px;gap:10px}footer strong{font-size:15px}footer span{font-size:14px}}",
].join("");

/** The preview and downloaded artifact use the same self-contained page. */
export function landingPage(photo: string) {
  return [
    '<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Fieldwork — Make room for your next idea</title><style>',
    pageCss,
    '</style></head><body><header><strong>fieldwork.</strong><span>A creative weekend by the sea</span></header><main class="hero"><div class="copy"><p class="eyebrow">Step out of the everyday.</p><h1>Make room<br>for your<br><em>next idea.</em></h1><p class="intro">Three days to explore, create, and connect with people who see things differently.</p><button class="join" id="join" type="button">Join the waitlist <span aria-hidden="true">↗</span></button><form hidden><label for="email">Your email</label><input type="email" id="email" name="email" autocomplete="email" placeholder="you@example.com" required><button type="submit">Join</button><p class="note">Demo signup. Your email stays in this preview.</p></form><p class="thanks" role="status" hidden>You’re on the preview list. This demo did not send or save your email.</p></div><img class="photo" src="',
    photo,
    '" alt="A sunlit studio terrace overlooking the Mediterranean Sea"></main><footer><div><strong>Make.</strong><span>Workshops & open studios</span></div><div><strong>Explore.</strong><span>Space for a fresh perspective</span></div><div><strong>Connect.</strong><span>A small, curious crowd</span></div></footer><script>const join=document.getElementById("join");const form=document.querySelector("form");join.addEventListener("click",()=>{join.hidden=true;form.hidden=false;document.getElementById("email").focus()});form.addEventListener("submit",event=>{event.preventDefault();form.hidden=true;document.querySelector(".thanks").hidden=false;form.reset()});</script></body></html>',
  ].join("");
}

export function campaignPoster(photo: string) {
  return [
    '<svg xmlns="http://www.w3.org/2000/svg" width="1080" height="1350" viewBox="0 0 1080 1350"><rect width="1080" height="1350" fill="#eae3d3"/><text x="70" y="105" font-family="Georgia,serif" font-size="82" fill="#293e31">fieldwork.</text><image href="',
    photo,
    '" x="35" y="160" width="1010" height="675" preserveAspectRatio="xMidYMid slice"/><text x="68" y="973" font-family="Georgia,serif" font-size="115" letter-spacing="-5" fill="#293e31">Make room for</text><text x="68" y="1100" font-family="Georgia,serif" font-size="115" font-style="italic" letter-spacing="-5" fill="#9a5238">your next idea.</text><text x="72" y="1215" font-family="Arial,sans-serif" font-size="76" fill="#56634e">A weekend by the sea.</text><text x="72" y="1300" font-family="Arial,sans-serif" font-size="76" fill="#293e31">Make. Explore. Connect.</text></svg>',
  ].join("");
}

export function taskCsv(completed: string[]) {
  return ["Task,Owner,Due,Status", ...launchTasks.map(task =>
    [task.title, task.owner, task.due, completed.includes(task.id) ? "Complete" : "To do"].map(value => '"' + value.replaceAll('"', '""') + '"').join(","),
  )].join("\r\n");
}
