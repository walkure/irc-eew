#!/usr/bin/env perl

# irc-eew.pl
# charset = utf-8
# walkure at 3pf.jp

use utf8;
use strict;
use warnings;
use IO::Select;
use YAML;
use Encode;
use File::Spec;
use File::Path qw/mkpath/;

use lib '.','./lib';
use Earthquake::EEW::Decoder;

use IRCSock;
use EEWSock;
use SlackWebhookSock;

binmode STDOUT,':utf8';

$SIG{'TERM'} = sub {die "SIGTERMed\n";};
$SIG{'INT'} = sub {die "SIGINTed\n";};

my $path = $ARGV[0] || 'config.yaml';

print "++Loading YAML:$path\n";
my $yaml = YAML::LoadFile($path);

my $eewlog = $yaml->{logdir};

if(defined $eewlog){
	print "++LogDir:[$eewlog]\n";
}else{
	print "++LogDir:N/A not save EEW data\n";
}

my $select = IO::Select->new();
print "++Get WNIEEW server list\n";
my $serv_list = EEWSock->get_server_list();
my $eewsock;

do{
	print "++Connect WNIEEW Server\n";
	$eewsock = EEWSock->new(
		ServerList=>$serv_list,
		Callback=>\&eew_callback,
		Logs=>$yaml->{WNIEEW}{Logs}
	);
}until(defined $eewsock);

my $eewdec = Earthquake::EEW::Decoder->new();

$select->add($eewsock);
$eewsock->logon($yaml->{WNIEEW});

my (@join_ch,%channel_noticeall,%channel_noticelimited,$ircsock);

if(defined $yaml->{irc}){
	$ircsock = IRCSock->new($yaml->{irc}{server});
	$select->add($ircsock);
	if(defined $yaml->{irc}{'all-notice'}){
		foreach my $ch(split(/,/,$yaml->{irc}{'all-notice'})){
			unless(defined $channel_noticeall{$ch} && defined $channel_noticelimited{$ch}){
				push(@join_ch,$ch);
				$channel_noticeall{$ch} = 1;
			}
		}
	}

	if(defined $yaml->{irc}{'limited-notice'}){
		foreach my $ch(split(/,/,$yaml->{irc}{'limited-notice'})){
			unless(defined $channel_noticeall{$ch} && defined $channel_noticelimited{$ch}){
				push(@join_ch,$ch);
				$channel_noticelimited{$ch} = 1;
			}
		}
	}

	$ircsock->login(\@join_ch);
}

my (@all_hooks,@limited_hooks);
if(defined $yaml->{slack}){
	push(@all_hooks,@{$yaml->{slack}{all}});
	push(@limited_hooks,@{$yaml->{slack}{limited}});
}

my $last_eq_id = '';

$! = 0;
while(my @ready = $select->can_read(60 * 3.5)) {
	if( length(@ready) == 0 ){
		die $! == 0 ?  "TCP Timeout..." : "Select Error:$!";
	}
	$! = 0;
	foreach my $sock(@ready){
		my $buf;
		my $len = $sock->sysread($buf,65535);
		
		if($len){
			$sock->parse_body($buf);
		}else{
			#connection closed
			print "++Connection closed...\n";
			$select->remove($sock);
			$sock->close();
			if(ref($sock) eq 'EEWSock'){
				do{
					$eewsock = EEWSock->new(
						ServerList => $serv_list,
						Callback=>\&eew_callback,
						Logs=>$yaml->{WNIEEW}{Logs}
					);
				}until(defined $eewsock);
				$select->add($eewsock);
				$eewsock->logon($yaml->{WNIEEW});
			}elsif(ref($sock) eq 'IRCSock'){
				$ircsock = IRCSock->new($yaml->{irc});
				$select->add($ircsock);
				$ircsock->login();
			}elsif(ref($sock) eq 'SlackWebhookSock'){
				my $res = $sock->get_response();
				print $sock->name.":[".$res->message."]\n";
			}
		}
	}
	if($! != 0){
		print "Errored:$!\n";
	}
	$! = 0;
}


sub eew_callback
{
	my($self,$md5,$buffer) = @_;
	
	my $tmpfname = time;
	$tmpfname .= ".$md5";
	if(defined $eewlog){
		print "+++Saved:$tmpfname\n";
		if(open(LOG,">$eewlog/$tmpfname")){
			print LOG $buffer;
			close LOG;
		}
	}
	
	my $d = $eewdec->read_data($buffer);

	my @wd = $d->{warn_time} =~ /\d\d/og;
	my @ed = $d->{eq_time}   =~ /\d\d/og;
	my $warnmsg = "20$wd[0]/$wd[1]/$wd[2] $wd[3]:$wd[4]:$wd[5]";
	my $eqedmsg = "20$ed[0]/$ed[1]/$wd[2] $ed[3]:$ed[4]:$ed[5]";

	my $times = ' 第'.$d->{'warn_num'}.'報';
	$times .= '(最終)' if $d->{NCN_type} > 0;

	my $msg;

	if($d->{'msg_type_code'} == 10){
		$msg = $warnmsg.$times.' ('.$eqedmsg.'発生) 取り消されました';
	}else{

		my $mapuri = sprintf 'http://maps.google.com/maps?q=%0.01f,%0.01f', $d->{'center_lat'},$d->{'center_lng'};
		my $center = sprintf('震央:<%s|N%.01f/E%0.01f>(%s)深さ%dkm',$mapuri,$d->{'center_lat'},$d->{'center_lng'},$d->{'center_name'},$d->{'center_depth'});
		my $magnitude = sprintf(' 最大:M%.01f 震度%s',$d->{'magnitude'},$d->{'shindo'});
		$msg = $warnmsg.$times.' ('.$eqedmsg.'発生)'.$center.$magnitude;
	}	
	
	my @notice_ch = keys %channel_noticeall;
#	push(@notice_ch,keys %channel_noticelimited) if($d->{'warn_num'} > 900 ||  $d->{'warn_num'} == 1 || $d->{'msg_type_code'} == 10 );

	my @notice_hook = @all_hooks;

	if($d->{eq_id} ne $last_eq_id || $d->{msg_type_code} == 10 || $d->{NCN_type} > 0){
		$last_eq_id = $d->{eq_id};
		push(@notice_ch,keys %channel_noticelimited);
		push(@notice_hook,@limited_hooks);
	}
	
	my sub remove_link {
		my $body = shift;
		$body =~ s/\<(.*?)\|(.*?)\>/$2/g;
		$body;
	}

	print "+++>>[$msg]\n";
	if(defined $ircsock){
		foreach my $ch (@notice_ch){
			print "++Noticed [$ch]\n";
			$ircsock->notice($ch, remove_link($msg));
		}
	}
	
	if(defined $yaml->{slack}){
		foreach my $endpoint (@notice_hook){
			my $webhsock = SlackWebhookSock->new($endpoint);
			next unless defined $webhsock;
			print '++Noticed ['.$webhsock->name."]\n";
			$webhsock->send_json({"text"=> $msg});
			$select->add($webhsock);
		}
		
	}

	if(defined $eewlog && -e "$eewlog/$tmpfname"){
		my $newname = $d->{eq_id}.'.'.$d->{warn_num};
		my $fullname = get_fn_from_eqid($eewlog,$newname);
		print "++Rename [$tmpfname] -> [$fullname]\n";
		rename "$eewlog/$tmpfname" , $fullname;
	}
	
}

sub get_fn_from_eqid
{
    my ($dir,$eqid) = @_;

    my $year = substr($eqid,0,4);
    my $month = substr($eqid,4,2);
    my $day = substr($eqid,6,2);

    my $fdir = File::Spec->catdir($dir,$year,$month,$day);

    mkpath($fdir);
    
	File::Spec->catfile($fdir,$eqid);
}
