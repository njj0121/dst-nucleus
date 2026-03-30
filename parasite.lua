-- WARNING: THIS IS NOT A MOD.
-- Do not place this file in the 'mods' folder.
-- This code is a payload injected directly into the DST process by an external application.

local fn = rawget(_G, "NUCLEUS_ACTIVATE");
if type(fn) == "function" then
	fn();
	return
end;
rawset(_G, "NUCLEUS_ACTIVATE", function()
	local w = rawget(_G, "TheWorld");
	if type(w) ~= "table" or not w.state then
		return
	end;
	local tuning = rawget(_G, "TUNING")
	local REAL_DAY_TIME = (type(tuning) == "table" and type(tuning.TOTAL_DAY_TIME) == "number") and tuning
		.TOTAL_DAY_TIME or 480

	local function GetDays(t)
		if type(t) ~= "number" then return 255 end;
		if t <= 0 then return 128 end;
		local d = math.ceil(t / REAL_DAY_TIME);
		if d > 126 then d = 126 end;
		return 128 + d;
	end;

	local function M(t)
		local m = math.floor(t / 60)
		if m > 126 then return 254 end
		if m < 0 then return 128 end
		return 128 + m
	end

	local function GetTimer(timer_name)
		if type(w.components.worldsettingstimer) ~= "table" then return 127 end
		local swt = w.components.worldsettingstimer
		if type(swt.GetTimeLeft) ~= "function" then return 255 end

		local ok, t = pcall(swt.GetTimeLeft, swt, timer_name)
		if ok and type(t) == "number" then
			return GetDays(t)
		end
		return 255
	end

	local c = w.state.cycles or 0;
	if c < 0 then
		c = 0
	elseif c > 2097151 then
		c = 2097151
	end;
	local gp = rawget(_G, "GetPlayerClientTable");
	local p = 0;
	if type(gp) == "function" then
		local pt = gp();
		if type(pt) == "table" then
			p = #pt
		end;
	end;
	if p > 127 then
		p = 127
	end;
	local s = w.state.season or "autumn";
	local m = w.state.moonphase or "new";
	local sv = 0;
	if s == "winter" then
		sv = 1
	elseif s == "spring" then
		sv = 2
	elseif s == "summer" then
		sv = 3
	end;
	local mv = 1;
	local clock_comp = w.net and w.net.components and w.net.components.clock;
	if clock_comp and type(clock_comp.GetDebugString) == "function" then
		local debug_str = clock_comp:GetDebugString();
		if type(debug_str) == "string" then
			local exact_phase_str = string.match(debug_str, "moon cycle: (%d+)");
			if exact_phase_str then
				local parsed_phase = tonumber(exact_phase_str);
				if parsed_phase and parsed_phase >= 1 and parsed_phase <= 20 then
					mv = parsed_phase;
				end
			end
		end
	end
	local env = 128 + (sv * 32) + mv;
	local d1 = 128 + math.floor(c / 16384);
	local d2 = 128 + math.floor(c / 128) % 128;
	local d3 = 128 + (c % 128);
	local ply = 128 + p;

	local raw_temp = math.floor(w.state.temperature or 0);
	local sign = 0;
	if raw_temp < 0 then
		sign = 1
	end;
	local abs_temp = math.abs(raw_temp);
	if abs_temp > 8191 then
		abs_temp = 8191
	end;
	local t1 = 128 + (sign * 64) + math.floor(abs_temp / 128);
	local t2 = 128 + (abs_temp % 128);

	local ph = 0;
	local wph = w.state.phase or "day";
	if wph == "dusk" then
		ph = 1
	elseif wph == "night" then
		ph = 2
	end;
	local rain = 0;
	if w.state.israining then
		rain = 1
	end;
	local snow = 0;
	if w.state.issnowing then
		snow = 1
	end;
	local nm = 0;
	local wnm = w.state.nightmarephase or "calm";
	if wnm == "warn" then
		nm = 1
	elseif wnm == "wild" then
		nm = 2
	elseif wnm == "dawn" then
		nm = 3
	elseif wnm ~= "calm" then
		nm = 4
	end;
	local byte8 = 128 + (ph * 32) + (rain * 16) + (snow * 8) + nm;
	local rem = math.floor(w.state.remainingdaysinseason or 0);
	if rem > 127 then
		rem = 127
	elseif rem < 0 then
		rem = 0
	end;
	local rem_b = 128 + rem;
	local aw = 0;
	if w.state.isalterawake then
		aw = 1
	end;
	local byte10 = 128 + aw;
	local env_payload = string.char(206, 223, 128, d1, d2, d3, ply, env, t1, t2, byte8, rem_b, byte10);
	local Ents = rawget(_G, "Ents") or {};

	local function FindEnt(prefab)
		for _, v in pairs(Ents) do
			if type(v) == "table" and v.prefab == prefab then
				return v
			end;
		end;
		return nil;
	end;
	local wc = w.components or {};
	local wt = wc.worldsettingstimer;

	-- 1. 巨鹿
	local b_deer = 255;
	if FindEnt("deerclops") then
		b_deer = 128;
	elseif type(w.components.deerclopsspawner) == "table" and type(w.components.deerclopsspawner.GetDebugString) == "function" then
		local dbg = w.components.deerclopsspawner:GetDebugString()
		if not string.find(dbg, "DORMANT") then
			local ok, t = pcall(wt.GetTimeLeft, wt, "deerclops_timetoattack")
			if ok and type(t) == "number" then
				b_deer = GetDays(t)
			elseif string.find(dbg, "ATTACKING") then
				b_deer = 128
			end
		end
	end
	-- 2. 熊大
	local b_bear = 255;
	if FindEnt("bearger") then
		b_bear = 128;
	elseif wt and type(wt.GetTimeLeft) == "function" then
		local ok, t = pcall(wt.GetTimeLeft, wt, "bearger_timetospawn");
		if ok and t then
			b_bear = GetDays(t)
		end;
	end;

	-- 3. 大鹅
	local b_goose = 255;
	if FindEnt("moose") or FindEnt("mooseegg") then
		b_goose = 128;
	else
		local nest = FindEnt("moose_nesting_ground");
		if nest and type(nest.components) == "table" and type(nest.components.timer) == "table" and type(nest.components.timer.GetTimeLeft) == "function" then
			local ok, t = pcall(nest.components.timer.GetTimeLeft, nest.components.timer, "CallMoose");
			if ok and t then
				b_goose = GetDays(t)
			end;
		end;
	end;

	-- 4. 龙蝇
	local b_fly = 255;
	if FindEnt("dragonfly") then
		b_fly = 128;
	else
		local spawner = FindEnt("dragonfly_spawner");
		if spawner then
			local is_cooling_down = false;
			if type(spawner.components) == "table" and type(spawner.components.worldsettingstimer) == "table" and type(spawner.components.worldsettingstimer.GetTimeLeft) == "function" then
				local swt = spawner.components.worldsettingstimer;
				local ok, t = pcall(swt.GetTimeLeft, swt, "regen_dragonfly");
				if ok and type(t) == "number" then
					b_fly = GetDays(t);
					is_cooling_down = true;
				end;
			end;
			if not is_cooling_down then
				b_fly = 128;
			end;
		end;
	end;

	-- 5. 蜂后
	local b_bee = 255;
	if FindEnt("beequeen") or FindEnt("beequeenhivegrown") then
		b_bee = 128;
	else
		local hive = FindEnt("beequeenhive");
		if hive and type(hive.components) == "table" and type(hive.components.timer) == "table" and type(hive.components.timer.GetTimeLeft) == "function" then
			local tmr = hive.components.timer;
			local ok, t = pcall(tmr.GetTimeLeft, tmr, "hivegrowth");
			if not (ok and t) then
				ok, t = pcall(tmr.GetTimeLeft, tmr, "hivegrowth2")
			end;
			if not (ok and t) then
				ok, t = pcall(tmr.GetTimeLeft, tmr, "hivegrowth1")
			end;
			if not (ok and t) then
				ok, t = pcall(tmr.GetTimeLeft, tmr, "shorthivegrowth")
			end;
			if not (ok and t) then
				ok, t = pcall(tmr.GetTimeLeft, tmr, "firsthivegrowth")
			end;
			if ok and t then
				b_bee = GetDays(t)
			end;
		end;
	end;

	-- 8. 克劳斯
	local b_klaus = 255;
	if FindEnt("klaus") then
		b_klaus = 128;
	elseif FindEnt("klaus_sack") then
		b_klaus = 128;
	else
		b_klaus = 255;
		if type(w.components.worldsettingstimer) == "table" and type(w.components.worldsettingstimer.GetTimeLeft) == "function" then
			local swt = w.components.worldsettingstimer;
			local ok, t = pcall(swt.GetTimeLeft, swt, "klaussack_spawntimer");
			if ok and type(t) == "number" then
				b_klaus = GetDays(t);
			end;
		end;
	end;

	-- 7. 蛤蟆
	local b_toad = 255;
	if FindEnt("toadstool") or FindEnt("toadstool_dark") then
		b_toad = 128;
	elseif w:HasTag("cave") then
		b_toad = 128;
		if wt and type(wt.GetTimeLeft) == "function" then
			local ok, t = pcall(wt.GetTimeLeft, wt, "toadstool_respawntask");
			if ok and type(t) == "number" then
				b_toad = GetDays(t);
			end;
		end;
	end;

	-- 8. 织影者
	local b_fw = 255;
	if FindEnt("stalker_atrium") then
		b_fw = 128;
	else
		local gate = FindEnt("atrium_gate");
		if gate then
			local is_ready = true;
			if type(gate.components) == "table" and type(gate.components.worldsettingstimer) == "table" and type(gate.components.worldsettingstimer.GetTimeLeft) == "function" then
				local gwt = gate.components.worldsettingstimer;
				local ok, t = pcall(gwt.GetTimeLeft, gwt, "cooldown");
				if ok and type(t) == "number" then
					b_fw = GetDays(t);
					is_ready = false;
				end;
			end;
			if is_ready then
				b_fw = 128;
			end;
		end;
	end;

	-- 9. 邪天翁
	local b_mal = 255
	if FindEnt("malbatross") then
		b_mal = 128
	elseif type(w.components.malbatrossspawner) == "table" and type(w.components.malbatrossspawner.GetDebugString) == "function" then
		local dbg = w.components.malbatrossspawner:GetDebugString()
		local tm = string.match(dbg, "in (%d+%.?%d*)")
		if tm then
			b_mal = GetDays(tonumber(tm))
		elseif string.find(dbg, "Spawning:") or string.find(dbg, "Trying to spawn:") then
			b_mal = 128
		end
	end

	-- 10. 果蝇王
	local b_flyking = 255
	if FindEnt("lordfruitfly") then
		b_flyking = 128
	else
		local t_fly = GetTimer("lordfruitfly_spawntime")
		if t_fly ~= 127 then
			b_flyking = t_fly
		elseif type(w.components.farming_manager) == "table" then
			b_flyking = 128
		end
	end

	-- 11. 蚁狮踩踏
	local b_antlion = 255
	local antlion = FindEnt("antlion")
	if antlion then
		if type(antlion.components.worldsettingstimer) == "table" and type(antlion.components.worldsettingstimer.GetTimeLeft) == "function" then
			local swt = antlion.components.worldsettingstimer
			local ok, t = pcall(swt.GetTimeLeft, swt, "rage")
			if ok and type(t) == "number" then
				b_antlion = M(t)
			end
		end
	end

	-- 判断洞穴
	local is_cave = w:HasTag("cave")
	local cmd_boss = is_cave and 130 or 129

	local boss_payload = string.char(206, 223, cmd_boss, b_deer, b_bear, b_goose, b_fly, b_bee, b_klaus, b_toad, b_fw,
		b_mal, b_flyking, b_antlion);

	local is_cave = w:HasTag("cave")
	if not is_cave then
		print(env_payload);
	end
	print(boss_payload);
end);
local NA = rawget(_G, "NUCLEUS_ACTIVATE");
NA();
local w = rawget(_G, "TheWorld");
if type(w) == "table" then
	w:ListenForEvent("cycleschanged", NA);
	w:ListenForEvent("playeractivated", NA);
	w:ListenForEvent("playerdeactivated", NA);
	w:ListenForEvent("season", NA);
	if w.nucleus_task then
		w.nucleus_task:Cancel()
	end;
	w.nucleus_task = w:DoPeriodicTask(5, NA);
end;
